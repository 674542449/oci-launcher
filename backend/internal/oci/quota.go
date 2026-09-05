package oci

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/limits"
)

type AccountTypeInfo struct {
	DetectedType    string `json:"detected_type"`    // FREE_TIER, PAYG, PROMOTION
	EffectiveType   string `json:"effective_type"`   // free or payg (after override)
	DetectionReason string `json:"detection_reason"` // Transparent proof
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
	TotalStorageGB    int64           `json:"total_storage_gb"` // Always 200 GB
	UsedStorageGB     int64           `json:"used_storage_gb"`
	AvailableStorage  int64           `json:"available_storage_gb"`
	OutboundTrafficTB float64         `json:"outbound_traffic_tb"` // Max 10 TB
	EstimatedMonthly  float64         `json:"estimated_monthly_fee"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// DetectAccountType executes account type detection via Limits API
func DetectAccountType(ctx context.Context, profile *storage.OCIProfile) (*AccountTypeInfo, error) {
	info := &AccountTypeInfo{
		DetectedType: "FREE_TIER",
	}

	// 1. Read standard-a1-core-count and memory from Limits API
	limitsClient, err := GetLimitsClient(profile)
	if err == nil {
		req := limits.ListLimitValuesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
			ServiceName:   common.String("compute"),
		}
		resp, err2 := limitsClient.ListLimitValues(ctx, req)
		if err2 == nil {
			for _, item := range resp.Items {
				name := StrVal(item.Name)
				if name == "standard-a1-core-count" && item.Value != nil {
					info.A1CoreLimit = *item.Value
				}
				if name == "standard-a1-memory-count" && item.Value != nil {
					info.A1MemoryLimit = *item.Value
				}
			}
		}
	}

	// Determine type based on A1 compute limits
	if info.A1CoreLimit >= 4 {
		info.DetectedType = "PAYG"
		info.DetectionReason = fmt.Sprintf("服务限额探测: standard-a1-core-count = %d (>= 4 判定为已升级 PAYG)", info.A1CoreLimit)
	} else {
		info.DetectedType = "FREE_TIER"
		info.DetectionReason = fmt.Sprintf("服务限额探测: standard-a1-core-count = %d (<= 2 判定为未升级免费号)", info.A1CoreLimit)
	}

	// Determine effective type based on manual override
	override := strings.ToLower(profile.AccountTypeOverride)
	if override == "payg" {
		info.EffectiveType = "payg"
		info.DetectionReason += " [用户已手动覆盖为: PAYG]"
	} else if override == "free" {
		info.EffectiveType = "free"
		info.DetectionReason += " [用户已手动覆盖为: FREE_TIER]"
	} else {
		if info.DetectedType == "PAYG" {
			info.EffectiveType = "payg"
		} else {
			info.EffectiveType = "free"
		}
	}

	return info, nil
}

// GetLiveQuotaSummary gathers all live usage concurrently via Goroutines (Scatter-Gather)
func GetLiveQuotaSummary(ctx context.Context, profile *storage.OCIProfile) (*QuotaSummary, error) {
	summary := &QuotaSummary{
		TotalStorageGB: 200,
		MaxMicroCount:  2,
		UpdatedAt:      time.Now(),
	}

	// 1. Detect account type
	typeInfo, err := DetectAccountType(ctx, profile)
	if err != nil {
		return nil, err
	}
	summary.AccountType = *typeInfo

	// Map free tier quota: Always Free baseline is 4 OCPU and 24 GB RAM
	summary.TotalFreeOCPU = 4.0
	summary.TotalFreeMemoryGB = 24.0
	if typeInfo.A1CoreLimit > 0 && float64(typeInfo.A1CoreLimit) < summary.TotalFreeOCPU {
		summary.TotalFreeOCPU = float64(typeInfo.A1CoreLimit)
	}
	if typeInfo.A1MemoryLimit > 0 && float64(typeInfo.A1MemoryLimit) < summary.TotalFreeMemoryGB {
		summary.TotalFreeMemoryGB = float64(typeInfo.A1MemoryLimit)
	}

	// 2. Fetch Home Region and Availability Domains
	var adNames []string
	idClient, err := GetIdentityClient(profile)
	if err == nil {
		tenancyResp, err2 := idClient.GetTenancy(ctx, identity.GetTenancyRequest{
			TenancyId: common.String(profile.TenancyOCID),
		})
		if err2 == nil && tenancyResp.HomeRegionKey != nil {
			summary.HomeRegion = *tenancyResp.HomeRegionKey
		}
		adResp, err3 := idClient.ListAvailabilityDomains(ctx, identity.ListAvailabilityDomainsRequest{
			CompartmentId: common.String(profile.TenancyOCID),
		})
		if err3 == nil {
			for _, ad := range adResp.Items {
				if ad.Name != nil {
					adNames = append(adNames, *ad.Name)
				}
			}
		}
	}
	if summary.HomeRegion == "" {
		summary.HomeRegion = profile.Region
	}

	// 3. Concurrent Scatter-Gather: Instances + Storage
	computeClient, err := GetComputeClient(profile, summary.HomeRegion)
	if err != nil {
		return nil, err
	}

	blockClient, err := GetBlockstorageClient(profile, summary.HomeRegion)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	var usedA1OCPU, usedA1Mem float64
	var microCount int
	var usedStorageGB int64
	var queryErr error
	var mu sync.Mutex

	// Query Instances
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := core.ListInstancesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
		}
		resp, err := computeClient.ListInstances(ctx, req)
		if err != nil {
			mu.Lock()
			queryErr = err
			mu.Unlock()
			return
		}

		var localOCPU, localMem float64
		var localMicro int

		for _, inst := range resp.Items {
			// Skip terminated instances
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

		mu.Lock()
		usedA1OCPU = localOCPU
		usedA1Mem = localMem
		microCount = localMicro
		mu.Unlock()
	}()

	// Query Boot Volumes and Block Volumes for storage summation
	wg.Add(1)
	go func() {
		defer wg.Done()
		var localStorage int64

		// 1. Boot volumes (must query by Availability Domain as required by OCI API)
		for _, ad := range adNames {
			bvReq := core.ListBootVolumesRequest{
				AvailabilityDomain: common.String(ad),
				CompartmentId:      common.String(profile.TenancyOCID),
			}
			bvResp, err := blockClient.ListBootVolumes(ctx, bvReq)
			if err == nil {
				for _, bv := range bvResp.Items {
					if bv.LifecycleState != core.BootVolumeLifecycleStateTerminated && bv.LifecycleState != core.BootVolumeLifecycleStateTerminating {
						if bv.SizeInGBs != nil {
							localStorage += *bv.SizeInGBs
						}
					}
				}
			}
		}

		// 2. Block volumes
		volReq := core.ListVolumesRequest{
			CompartmentId: common.String(profile.TenancyOCID),
		}
		volResp, err := blockClient.ListVolumes(ctx, volReq)
		if err == nil {
			for _, vol := range volResp.Items {
				if vol.LifecycleState != core.VolumeLifecycleStateTerminated && vol.LifecycleState != core.VolumeLifecycleStateTerminating {
					if vol.SizeInGBs != nil {
						localStorage += *vol.SizeInGBs
					}
				}
			}
		}

		mu.Lock()
		usedStorageGB = localStorage
		mu.Unlock()
	}()

	wg.Wait()

	if queryErr != nil {
		return nil, queryErr
	}

	summary.UsedA1OCPU = usedA1OCPU
	summary.UsedA1MemoryGB = usedA1Mem
	summary.MicroCount = microCount
	summary.UsedStorageGB = usedStorageGB

	summary.AvailableA1OCPU = summary.TotalFreeOCPU - summary.UsedA1OCPU
	if summary.AvailableA1OCPU < 0 {
		summary.AvailableA1OCPU = 0
	}
	summary.AvailableA1Memory = summary.TotalFreeMemoryGB - summary.UsedA1MemoryGB
	if summary.AvailableA1Memory < 0 {
		summary.AvailableA1Memory = 0
	}
	summary.AvailableStorage = summary.TotalStorageGB - summary.UsedStorageGB
	if summary.AvailableStorage < 0 {
		summary.AvailableStorage = 0
	}

	// Calculate PAYG estimated monthly fee if usage exceeds free threshold
	if summary.UsedA1OCPU > summary.TotalFreeOCPU || summary.UsedA1MemoryGB > summary.TotalFreeMemoryGB {
		extraOCPU := summary.UsedA1OCPU - summary.TotalFreeOCPU
		if extraOCPU < 0 {
			extraOCPU = 0
		}
		extraMem := summary.UsedA1MemoryGB - summary.TotalFreeMemoryGB
		if extraMem < 0 {
			extraMem = 0
		}
		// $0.01/OCPU-hour + $0.0015/GB-hour across 730 hours
		summary.EstimatedMonthly = (extraOCPU*0.01 + extraMem*0.0015) * 730.0
	}

	return summary, nil
}

// ValidateFreeTierConstraint enforces strict zero-cost boundary before creating an instance task
func ValidateFreeTierConstraint(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask) error {
	summary, err := GetLiveQuotaSummary(ctx, profile)
	if err != nil {
		return fmt.Errorf("无法获取账号当前配额: %w", err)
	}

	// 1. Home Region Rule: Free tier only valid in Home Region
	if summary.HomeRegion != "" && !strings.EqualFold(task.Region, summary.HomeRegion) && !strings.Contains(strings.ToLower(task.Region), strings.ToLower(summary.HomeRegion)) {
		return fmt.Errorf("【零费用硬阻断】目标区域 [%s] 与该账号的主区域 (Home Region: [%s]) 不一致！跨区创建将失去免费资格并产生扣费，系统已严格阻断", task.Region, summary.HomeRegion)
	}

	// 2. Storage Rule: Total storage <= 200 GB
	if summary.UsedStorageGB+task.BootVolumeSizeInGBs > 200 {
		return fmt.Errorf("【存储超额硬阻断】申请引导卷 %d GB + 当前已用存储 %d GB = %d GB，超过了 200 GB 免费存储总限额！请调整引导卷大小",
			task.BootVolumeSizeInGBs, summary.UsedStorageGB, summary.UsedStorageGB+task.BootVolumeSizeInGBs)
	}

	// 3. Compute Rule
	if strings.Contains(task.Shape, "A1.Flex") {
		maxOCPU := summary.TotalFreeOCPU
		maxMem := summary.TotalFreeMemoryGB

		if summary.UsedA1OCPU+task.OCPU > maxOCPU {
			return fmt.Errorf("【CPU额度硬阻断】申请 %0.1f OCPU + 当前已用 %0.1f OCPU = %0.1f OCPU，已超出当前账号免费额度 (%0.1f OCPU)",
				task.OCPU, summary.UsedA1OCPU, summary.UsedA1OCPU+task.OCPU, maxOCPU)
		}

		if summary.UsedA1MemoryGB+task.MemoryInGBs > maxMem {
			return fmt.Errorf("【内存额度硬阻断】申请 %0.1f GB + 当前已用 %0.1f GB = %0.1f GB，已超出当前账号免费额度 (%0.1f GB)",
				task.MemoryInGBs, summary.UsedA1MemoryGB, summary.UsedA1MemoryGB+task.MemoryInGBs, maxMem)
		}
	} else if strings.Contains(task.Shape, "E2.1.Micro") {
		if summary.MicroCount >= 2 {
			return fmt.Errorf("【AMD数量硬阻断】当前已有 %d 台 VM.Standard.E2.1.Micro 实例，已达到 2 台免费上限", summary.MicroCount)
		}
	}

	return nil
}
