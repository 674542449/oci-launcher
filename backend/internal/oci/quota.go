package oci

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/limits"
)

// Documented PAYG default for standard-a1-core-count (Service Limits reference). Always Free
// tenancies get a small fixed cap instead, so anything above this clearly indicates an upgraded account.
const paygA1CoreLimitHint = 4

type AccountTypeInfo struct {
	DetectedType    string `json:"detected_type"`    // FREE_TIER, PAYG, UNKNOWN
	EffectiveType   string `json:"effective_type"`   // free or payg (after override)
	DetectionReason string `json:"detection_reason"` // Transparent proof
	DetectionSource string `json:"detection_source"` // subscription (Organizations API) or limits (fallback)
	A1CoreLimit     int64  `json:"a1_core_limit"`
	A1MemoryLimit   int64  `json:"a1_memory_limit"`
}

type QuotaSummary struct {
	AccountType       AccountTypeInfo `json:"account_type"`
	HomeRegion        string          `json:"home_region"`
	TotalFreeOCPU     float64         `json:"total_free_ocpu"`
	UsedA1OCPU        float64         `json:"used_a1_ocpu"`
	AvailableA1OCPU   float64         `json:"available_a1_ocpu"`
	TotalFreeMemoryGB float64         `json:"total_free_memory_gb"`
	UsedA1MemoryGB    float64         `json:"used_a1_memory_gb"`
	AvailableA1Memory float64         `json:"available_a1_memory_gb"`
	MicroCount        int             `json:"micro_count"`
	MaxMicroCount     int             `json:"max_micro_count"`
	TotalStorageGB    int64           `json:"total_storage_gb"`
	UsedStorageGB     int64           `json:"used_storage_gb"`
	AvailableStorage  int64           `json:"available_storage_gb"`
	OutboundTrafficTB float64         `json:"outbound_traffic_tb"` // Max 10 TB
	EstimatedMonthly  float64         `json:"estimated_monthly_fee"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// a1Allowance returns the free A1 allowance for an account type:
// Always Free tenancies get 2 OCPU / 12 GB, upgraded PAYG tenancies 4 OCPU / 24 GB (configurable).
func a1Allowance(effectiveType string) (ocpu, memGB float64) {
	ocpu, memGB = 2, 12
	if effectiveType == "payg" {
		ocpu, memGB = 4, 24
	}
	if cfg := config.GlobalConfig; cfg != nil {
		if effectiveType == "payg" {
			if cfg.PaygA1OCPU > 0 {
				ocpu = cfg.PaygA1OCPU
			}
			if cfg.PaygA1MemoryGB > 0 {
				memGB = cfg.PaygA1MemoryGB
			}
		} else {
			if cfg.FreeA1OCPU > 0 {
				ocpu = cfg.FreeA1OCPU
			}
			if cfg.FreeA1MemoryGB > 0 {
				memGB = cfg.FreeA1MemoryGB
			}
		}
	}
	return
}

// sharedAllowance returns the allowances that do not depend on the account type.
func sharedAllowance() (storageGB int64, micro int) {
	storageGB, micro = 200, 2
	if cfg := config.GlobalConfig; cfg != nil {
		if cfg.FreeStorageGB > 0 {
			storageGB = cfg.FreeStorageGB
		}
		if cfg.FreeMicroCount > 0 {
			micro = cfg.FreeMicroCount
		}
	}
	return
}

// ResolveHomeRegion returns the tenancy's home region *name* (e.g. "ap-tokyo-1").
// GetTenancy only exposes the 3-letter key ("NRT"), which must not be compared with region names.
func ResolveHomeRegion(ctx context.Context, profile *storage.OCIProfile) (string, error) {
	cacheKey := "homeregion:" + profile.TenancyOCID
	if cached, err := cache.GetCachedMetadata(ctx, cacheKey); err == nil && cached != "" {
		return cached, nil
	}

	idClient, err := GetIdentityClient(profile)
	if err != nil {
		return "", err
	}

	homeRegion := ""
	subs, err := idClient.ListRegionSubscriptions(ctx, identity.ListRegionSubscriptionsRequest{
		TenancyId: common.String(profile.TenancyOCID),
	})
	if err == nil {
		for _, s := range subs.Items {
			if BoolVal(s.IsHomeRegion) && s.RegionName != nil {
				homeRegion = *s.RegionName
				break
			}
		}
	}

	if homeRegion == "" {
		// Fallback: map the region key through the SDK's table
		tenancyResp, err2 := idClient.GetTenancy(ctx, identity.GetTenancyRequest{
			TenancyId: common.String(profile.TenancyOCID),
		})
		if err2 != nil {
			if err != nil {
				return "", fmt.Errorf("failed to resolve home region: %w", err)
			}
			return "", fmt.Errorf("failed to resolve home region: %w", err2)
		}
		if tenancyResp.HomeRegionKey != nil {
			homeRegion = string(common.StringToRegion(*tenancyResp.HomeRegionKey))
		}
	}

	if homeRegion == "" {
		return "", fmt.Errorf("tenancy has no home region subscription")
	}

	_ = cache.CacheMetadata(ctx, cacheKey, homeRegion, 24*time.Hour)
	return homeRegion, nil
}

// fetchA1Limits reads the tenancy's standard-a1-core-count / standard-a1-memory-count limits in
// the given region. Limits are AD-scoped, so the maximum across ADs is returned.
func fetchA1Limits(ctx context.Context, profile *storage.OCIProfile, region string) (coreLimit, memLimit int64, err error) {
	limitsClient, err := GetLimitsClient(profile, region)
	if err != nil {
		return 0, 0, err
	}

	maxFor := func(name string) (int64, error) {
		req := limits.ListLimitValuesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
			ServiceName:   common.String("compute"),
			Name:          common.String(name),
			Limit:         common.Int(100),
		}
		var best int64
		for {
			resp, err := limitsClient.ListLimitValues(ctx, req)
			if err != nil {
				return 0, err
			}
			for _, item := range resp.Items {
				if item.Value != nil && *item.Value > best {
					best = *item.Value
				}
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
		return best, nil
	}

	if coreLimit, err = maxFor("standard-a1-core-count"); err != nil {
		return 0, 0, err
	}
	if memLimit, err = maxFor("standard-a1-memory-count"); err != nil {
		return coreLimit, 0, err
	}
	return coreLimit, memLimit, nil
}

// DetectAccountType classifies the tenancy. The account-level answer comes from the
// Organizations subscription API (free promotion vs. paid subscription); the A1 service limit
// is only consulted when that API is unavailable, and the source is reported either way.
func DetectAccountType(ctx context.Context, profile *storage.OCIProfile, homeRegion string) (*AccountTypeInfo, error) {
	info := &AccountTypeInfo{DetectedType: "UNKNOWN"}

	// A1 limits are still reported for transparency (and as the fallback signal)
	coreLimit, memLimit, limitsErr := fetchA1Limits(ctx, profile, homeRegion)
	if limitsErr == nil {
		info.A1CoreLimit = coreLimit
		info.A1MemoryLimit = memLimit
	}

	verdict, subErr := DetectAccountTypeBySubscription(ctx, profile, homeRegion)
	switch {
	case subErr == nil && verdict != nil && verdict.Decided:
		info.DetectionSource = "subscription"
		if verdict.IsPaid {
			info.DetectedType = "PAYG"
		} else {
			info.DetectedType = "FREE_TIER"
		}
		info.DetectionReason = verdict.Reason
	default:
		info.DetectionSource = "limits"
		why := "订阅接口未能判定"
		if subErr != nil {
			why = "订阅接口不可用 (" + subErr.Error() + ")"
		} else if verdict != nil && !verdict.Found {
			why = "订阅接口未返回订阅"
		}
		if limitsErr != nil {
			info.DetectionReason = fmt.Sprintf("%s；服务限额查询也失败，无法自动判定: %v。可手动覆盖账号类型", why, limitsErr)
		} else if coreLimit > paygA1CoreLimitHint {
			info.DetectedType = "PAYG"
			info.DetectionReason = fmt.Sprintf("%s；按服务限额推断: standard-a1-core-count = %d（大于 %d，判定为已升级 PAYG）", why, coreLimit, paygA1CoreLimitHint)
		} else {
			info.DetectedType = "FREE_TIER"
			info.DetectionReason = fmt.Sprintf("%s；按服务限额推断: standard-a1-core-count = %d（不超过 %d，判定为 Always Free）", why, coreLimit, paygA1CoreLimitHint)
		}
	}

	// Persist for the account list
	storage.DB.Model(profile).Updates(map[string]interface{}{
		"detected_type":    info.DetectedType,
		"detection_reason": info.DetectionReason,
		"detection_source": info.DetectionSource,
	})

	override := strings.ToLower(profile.AccountTypeOverride)
	switch override {
	case "payg":
		info.EffectiveType = "payg"
		info.DetectionReason += " [已手动覆盖为 PAYG]"
	case "free":
		info.EffectiveType = "free"
		info.DetectionReason += " [已手动覆盖为 Always Free]"
	default:
		if info.DetectedType == "PAYG" {
			info.EffectiveType = "payg"
		} else {
			info.EffectiveType = "free"
		}
	}

	return info, nil
}

// GetLiveQuotaSummary gathers live usage in the home region (instances + boot/block volumes).
func GetLiveQuotaSummary(ctx context.Context, profile *storage.OCIProfile) (*QuotaSummary, error) {
	freeStorage, freeMicro := sharedAllowance()

	summary := &QuotaSummary{
		TotalStorageGB: freeStorage,
		MaxMicroCount:  freeMicro,
		UpdatedAt:      time.Now(),
	}

	// 1. Home region (Always Free resources only exist there)
	homeRegion, err := ResolveHomeRegion(ctx, profile)
	if err != nil {
		return nil, err
	}
	summary.HomeRegion = homeRegion

	// 2. Account type from service limits (with manual override applied)
	typeInfo, err := DetectAccountType(ctx, profile, homeRegion)
	if err != nil {
		return nil, err
	}
	summary.AccountType = *typeInfo

	// 3. A1 allowance depends on the effective account type: free 2/12, upgraded PAYG 4/24
	summary.TotalFreeOCPU, summary.TotalFreeMemoryGB = a1Allowance(typeInfo.EffectiveType)

	// A tenancy cap below the allowance wins; a higher cap never raises the free line.
	if typeInfo.A1CoreLimit > 0 && float64(typeInfo.A1CoreLimit) < summary.TotalFreeOCPU {
		summary.TotalFreeOCPU = float64(typeInfo.A1CoreLimit)
	}
	if typeInfo.A1MemoryLimit > 0 && float64(typeInfo.A1MemoryLimit) < summary.TotalFreeMemoryGB {
		summary.TotalFreeMemoryGB = float64(typeInfo.A1MemoryLimit)
	}

	// 3. Availability domains of the home region (AD names are region specific)
	adNames, err := ListAvailabilityDomainNames(ctx, profile, homeRegion)
	if err != nil {
		return nil, fmt.Errorf("failed to list availability domains in %s: %w", homeRegion, err)
	}

	computeClient, err := GetComputeClient(profile, homeRegion)
	if err != nil {
		return nil, err
	}
	blockClient, err := GetBlockstorageClient(profile, homeRegion)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var usedA1OCPU, usedA1Mem float64
	var microCount int
	var usedStorageGB int64
	var errs []error
	fail := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	// Instances
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := core.ListInstancesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
			Limit:         common.Int(100),
		}
		var localOCPU, localMem float64
		var localMicro int
		for {
			resp, err := computeClient.ListInstances(ctx, req)
			if err != nil {
				fail(fmt.Errorf("list instances: %w", err))
				return
			}
			for _, inst := range resp.Items {
				if inst.LifecycleState == core.InstanceLifecycleStateTerminated || inst.LifecycleState == core.InstanceLifecycleStateTerminating {
					continue
				}
				shape := StrVal(inst.Shape)
				if strings.Contains(shape, "A1.Flex") {
					if inst.ShapeConfig != nil {
						if inst.ShapeConfig.Ocpus != nil {
							localOCPU += float64(*inst.ShapeConfig.Ocpus)
						}
						if inst.ShapeConfig.MemoryInGBs != nil {
							localMem += float64(*inst.ShapeConfig.MemoryInGBs)
						}
					}
				} else if strings.Contains(shape, "E2.1.Micro") {
					localMicro++
				}
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
		mu.Lock()
		usedA1OCPU, usedA1Mem, microCount = localOCPU, localMem, localMicro
		mu.Unlock()
	}()

	// Boot volumes (per AD) + block volumes
	wg.Add(1)
	go func() {
		defer wg.Done()
		var localStorage int64
		for _, ad := range adNames {
			bvReq := core.ListBootVolumesRequest{
				AvailabilityDomain: common.String(ad),
				CompartmentId:      common.String(profile.TenancyOCID),
				Limit:              common.Int(100),
			}
			for {
				bvResp, err := blockClient.ListBootVolumes(ctx, bvReq)
				if err != nil {
					fail(fmt.Errorf("list boot volumes in %s: %w", ad, err))
					return
				}
				for _, bv := range bvResp.Items {
					if bv.LifecycleState != core.BootVolumeLifecycleStateTerminated && bv.LifecycleState != core.BootVolumeLifecycleStateTerminating {
						localStorage += Int64Val(bv.SizeInGBs)
					}
				}
				if bvResp.OpcNextPage == nil {
					break
				}
				bvReq.Page = bvResp.OpcNextPage
			}
		}

		volReq := core.ListVolumesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
			Limit:         common.Int(100),
		}
		for {
			volResp, err := blockClient.ListVolumes(ctx, volReq)
			if err != nil {
				fail(fmt.Errorf("list block volumes: %w", err))
				return
			}
			for _, vol := range volResp.Items {
				if vol.LifecycleState != core.VolumeLifecycleStateTerminated && vol.LifecycleState != core.VolumeLifecycleStateTerminating {
					localStorage += Int64Val(vol.SizeInGBs)
				}
			}
			if volResp.OpcNextPage == nil {
				break
			}
			volReq.Page = volResp.OpcNextPage
		}

		mu.Lock()
		usedStorageGB = localStorage
		mu.Unlock()
	}()

	wg.Wait()

	if len(errs) > 0 {
		// Fail closed: an incomplete picture must not pass the zero-cost guard.
		return nil, errs[0]
	}

	summary.UsedA1OCPU = usedA1OCPU
	summary.UsedA1MemoryGB = usedA1Mem
	summary.MicroCount = microCount
	summary.UsedStorageGB = usedStorageGB

	summary.AvailableA1OCPU = maxFloat(0, summary.TotalFreeOCPU-summary.UsedA1OCPU)
	summary.AvailableA1Memory = maxFloat(0, summary.TotalFreeMemoryGB-summary.UsedA1MemoryGB)
	summary.AvailableStorage = summary.TotalStorageGB - summary.UsedStorageGB
	if summary.AvailableStorage < 0 {
		summary.AvailableStorage = 0
	}

	// PAYG estimate for usage above the free line: $0.01/OCPU-hour + $0.0015/GB-hour, 730 h/month
	extraOCPU := maxFloat(0, summary.UsedA1OCPU-summary.TotalFreeOCPU)
	extraMem := maxFloat(0, summary.UsedA1MemoryGB-summary.TotalFreeMemoryGB)
	if extraOCPU > 0 || extraMem > 0 {
		summary.EstimatedMonthly = (extraOCPU*0.01 + extraMem*0.0015) * 730.0
	}

	return summary, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ValidateFreeTierConstraint enforces the zero-cost boundary before a launch task is created or resumed.
func ValidateFreeTierConstraint(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask) error {
	summary, err := GetLiveQuotaSummary(ctx, profile)
	if err != nil {
		return fmt.Errorf("无法获取账号当前配额: %w", err)
	}

	// 1. Always Free resources only exist in the home region
	if summary.HomeRegion != "" && !strings.EqualFold(strings.TrimSpace(task.Region), summary.HomeRegion) {
		return fmt.Errorf("【零费用硬阻断】目标区域 [%s] 与该账号的主区域 [%s] 不一致。免费资源只能在主区域创建，跨区创建会产生扣费", task.Region, summary.HomeRegion)
	}

	// 2. Boot + block storage total
	if summary.UsedStorageGB+task.BootVolumeSizeInGBs > summary.TotalStorageGB {
		return fmt.Errorf("【存储超额硬阻断】申请引导卷 %d GB + 当前已用 %d GB = %d GB，超过 %d GB 免费存储总额，请调小引导卷",
			task.BootVolumeSizeInGBs, summary.UsedStorageGB, summary.UsedStorageGB+task.BootVolumeSizeInGBs, summary.TotalStorageGB)
	}

	// 3. Compute
	if strings.Contains(task.Shape, "A1.Flex") {
		if summary.UsedA1OCPU+task.OCPU > summary.TotalFreeOCPU {
			return fmt.Errorf("【CPU额度硬阻断】申请 %0.1f OCPU + 当前已用 %0.1f OCPU = %0.1f OCPU，超出免费额度 %0.1f OCPU",
				task.OCPU, summary.UsedA1OCPU, summary.UsedA1OCPU+task.OCPU, summary.TotalFreeOCPU)
		}
		if summary.UsedA1MemoryGB+task.MemoryInGBs > summary.TotalFreeMemoryGB {
			return fmt.Errorf("【内存额度硬阻断】申请 %0.1f GB + 当前已用 %0.1f GB = %0.1f GB，超出免费额度 %0.1f GB",
				task.MemoryInGBs, summary.UsedA1MemoryGB, summary.UsedA1MemoryGB+task.MemoryInGBs, summary.TotalFreeMemoryGB)
		}
	} else if strings.Contains(task.Shape, "E2.1.Micro") {
		if summary.MicroCount >= summary.MaxMicroCount {
			return fmt.Errorf("【AMD数量硬阻断】当前已有 %d 台 VM.Standard.E2.1.Micro 实例，已达到 %d 台免费上限", summary.MicroCount, summary.MaxMicroCount)
		}
	}

	return nil
}
