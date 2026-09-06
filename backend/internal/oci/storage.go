package oci

import (
	"context"
	"fmt"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
)

type BootVolumeItem struct {
	OCID         string `json:"ocid"`
	DisplayName  string `json:"display_name"`
	SizeInGBs    int64  `json:"size_in_gbs"`
	VpusPerGB    int64  `json:"vpus_per_gb"` // 10 to 120 (boot volumes require min 10 VPU)
	State        string `json:"state"`
	AD           string `json:"ad"`
	TimeCreated  string `json:"time_created"`
	GrowCommands string `json:"grow_commands"`
}

type BlockVolumeItem struct {
	OCID        string `json:"ocid"`
	DisplayName string `json:"display_name"`
	SizeInGBs   int64  `json:"size_in_gbs"`
	VpusPerGB   int64  `json:"vpus_per_gb"`
	State       string `json:"state"`
	AD          string `json:"ad"`
	TimeCreated string `json:"time_created"`
}

type VolumeBackupItem struct {
	OCID         string `json:"ocid"`
	DisplayName  string `json:"display_name"`
	VolumeType   string `json:"volume_type"` // BootVolume or BlockVolume
	SourceVolume string `json:"source_volume"`
	SizeInGBs    int64  `json:"size_in_gbs"`
	State        string `json:"state"`
	TimeCreated  string `json:"time_created"`
}

type BucketItem struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	StorageTier  string `json:"storage_tier"`
	PublicAccess string `json:"public_access"`
	ApproxSizeGB int64  `json:"approx_size_gb"`
	TimeCreated  string `json:"time_created"`
}

const growCommands = "# Ubuntu / Oracle Linux: extend the root partition and filesystem after resizing the boot volume\nsudo oci-growfs -y 2>/dev/null || (sudo apt-get install -y cloud-guest-utils && sudo growpart /dev/sda 1 && sudo resize2fs /dev/sda1)"

func fmtTime(t *common.SDKTime) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ListBootVolumes lists boot volumes across all availability domains of the region (all pages)
func ListBootVolumes(ctx context.Context, profile *storage.OCIProfile, region string) ([]BootVolumeItem, error) {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	adNames, err := ListAvailabilityDomainNames(ctx, profile, region)
	if err != nil {
		return nil, fmt.Errorf("failed to list availability domains: %w", err)
	}

	items := []BootVolumeItem{}
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		for _, ad := range adNames {
			req := core.ListBootVolumesRequest{
				AvailabilityDomain: common.String(ad),
				CompartmentId:      common.String(comp.ID),
				Limit:              common.Int(100),
			}
			for {
				resp, err := blockClient.ListBootVolumes(ctx, req)
				if err != nil {
					if skipUnreadableCompartment(comp, profile, err) {
						continue comps
					}
					return nil, fmt.Errorf("failed to list boot volumes in %s: %w", ad, err)
				}
				for _, bv := range resp.Items {
					if bv.LifecycleState == core.BootVolumeLifecycleStateTerminated {
						continue
					}
					vpu := Int64Val(bv.VpusPerGB)
					if bv.VpusPerGB == nil {
						vpu = 10
					}
					items = append(items, BootVolumeItem{
						OCID:         StrVal(bv.Id),
						DisplayName:  StrVal(bv.DisplayName),
						SizeInGBs:    Int64Val(bv.SizeInGBs),
						VpusPerGB:    vpu,
						State:        string(bv.LifecycleState),
						AD:           StrVal(bv.AvailabilityDomain),
						TimeCreated:  fmtTime(bv.TimeCreated),
						GrowCommands: growCommands,
					})
				}
				if resp.OpcNextPage == nil {
					break
				}
				req.Page = resp.OpcNextPage
			}
		}
	}

	return items, nil
}

// ResizeBootVolume updates boot volume size and performance (10-120 VPU/GB in steps of 10)
func ResizeBootVolume(ctx context.Context, profile *storage.OCIProfile, region, bootVolumeOCID string, newSizeGB, newVPU int64) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	_, err = blockClient.UpdateBootVolume(ctx, core.UpdateBootVolumeRequest{
		BootVolumeId: common.String(bootVolumeOCID),
		UpdateBootVolumeDetails: core.UpdateBootVolumeDetails{
			SizeInGBs: common.Int64(newSizeGB),
			VpusPerGB: common.Int64(normalizeVPU(newVPU, false)),
		},
	})
	return err
}

// CreateBootVolumeBackup creates a full backup
func CreateBootVolumeBackup(ctx context.Context, profile *storage.OCIProfile, region, bootVolumeOCID, name string) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	_, err = blockClient.CreateBootVolumeBackup(ctx, core.CreateBootVolumeBackupRequest{
		CreateBootVolumeBackupDetails: core.CreateBootVolumeBackupDetails{
			BootVolumeId: common.String(bootVolumeOCID),
			DisplayName:  common.String(name),
			Type:         core.CreateBootVolumeBackupDetailsTypeFull,
		},
	})
	return err
}

// ListBlockVolumes lists block volumes (all pages)
func ListBlockVolumes(ctx context.Context, profile *storage.OCIProfile, region string) ([]BlockVolumeItem, error) {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	items := []BlockVolumeItem{}
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		req := core.ListVolumesRequest{
			CompartmentId: common.String(comp.ID),
			Limit:         common.Int(100),
		}
		for {
			resp, err := blockClient.ListVolumes(ctx, req)
			if err != nil {
				if skipUnreadableCompartment(comp, profile, err) {
					continue comps
				}
				return nil, err
			}
			for _, vol := range resp.Items {
				if vol.LifecycleState == core.VolumeLifecycleStateTerminated {
					continue
				}
				items = append(items, BlockVolumeItem{
					OCID:        StrVal(vol.Id),
					DisplayName: StrVal(vol.DisplayName),
					SizeInGBs:   Int64Val(vol.SizeInGBs),
					VpusPerGB:   Int64Val(vol.VpusPerGB),
					State:       string(vol.LifecycleState),
					AD:          StrVal(vol.AvailabilityDomain),
					TimeCreated: fmtTime(vol.TimeCreated),
				})
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
	}

	return items, nil
}

// CreateBlockVolume creates a new block volume (0 = lower cost, 10 = balanced, 20 = higher, 30-120 = ultra high)
func CreateBlockVolume(ctx context.Context, profile *storage.OCIProfile, region, ad, name string, sizeGB, vpu int64) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	_, err = blockClient.CreateVolume(ctx, core.CreateVolumeRequest{
		CreateVolumeDetails: core.CreateVolumeDetails{
			CompartmentId:      common.String(profile.TenancyOCID),
			AvailabilityDomain: common.String(ad),
			DisplayName:        common.String(name),
			SizeInGBs:          common.Int64(sizeGB),
			VpusPerGB:          common.Int64(normalizeVPU(vpu, true)),
		},
	})
	return err
}

func getNamespace(ctx context.Context, osClient objectstorage.ObjectStorageClient) (string, error) {
	nsResp, err := osClient.GetNamespace(ctx, objectstorage.GetNamespaceRequest{})
	if err != nil {
		return "", fmt.Errorf("failed to get object storage namespace: %w", err)
	}
	if nsResp.Value == nil || *nsResp.Value == "" {
		return "", fmt.Errorf("failed to get object storage namespace: empty response")
	}
	return *nsResp.Value, nil
}

// ListBuckets lists Object Storage buckets with tier, access type and approximate size
func ListBuckets(ctx context.Context, profile *storage.OCIProfile, region string) ([]BucketItem, error) {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	namespace, err := getNamespace(ctx, osClient)
	if err != nil {
		return nil, err
	}

	var summaries []objectstorage.BucketSummary
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		req := objectstorage.ListBucketsRequest{
			NamespaceName: common.String(namespace),
			CompartmentId: common.String(comp.ID),
			Limit:         common.Int(100),
		}
		for {
			resp, err := osClient.ListBuckets(ctx, req)
			if err != nil {
				if skipUnreadableCompartment(comp, profile, err) {
					continue comps
				}
				return nil, err
			}
			summaries = append(summaries, resp.Items...)
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
	}

	items := make([]BucketItem, 0, len(summaries))
	for i, b := range summaries {
		item := BucketItem{
			Name:        StrVal(b.Name),
			Namespace:   namespace,
			TimeCreated: fmtTime(b.TimeCreated),
		}
		// Tier / access / size only exist on the full Bucket object (one extra call per bucket)
		if i < 50 && b.Name != nil {
			detail, err := osClient.GetBucket(ctx, objectstorage.GetBucketRequest{
				NamespaceName: common.String(namespace),
				BucketName:    b.Name,
				Fields:        []objectstorage.GetBucketFieldsEnum{objectstorage.GetBucketFieldsApproximatesize},
			})
			if err == nil {
				item.StorageTier = string(detail.Bucket.StorageTier)
				item.PublicAccess = string(detail.Bucket.PublicAccessType)
				if detail.Bucket.ApproximateSize != nil {
					item.ApproxSizeGB = *detail.Bucket.ApproximateSize / (1024 * 1024 * 1024)
				}
			}
		}
		items = append(items, item)
	}

	return items, nil
}

// CreateBucket creates a private Standard-tier bucket
func CreateBucket(ctx context.Context, profile *storage.OCIProfile, region, bucketName string) error {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return err
	}

	namespace, err := getNamespace(ctx, osClient)
	if err != nil {
		return err
	}

	_, err = osClient.CreateBucket(ctx, objectstorage.CreateBucketRequest{
		NamespaceName: common.String(namespace),
		CreateBucketDetails: objectstorage.CreateBucketDetails{
			Name:             common.String(bucketName),
			CompartmentId:    common.String(profile.TenancyOCID),
			PublicAccessType: objectstorage.CreateBucketDetailsPublicAccessTypeNopublicaccess,
			StorageTier:      objectstorage.CreateBucketDetailsStorageTierStandard,
		},
	})
	return err
}

// DeleteBucket deletes an (empty) bucket
func DeleteBucket(ctx context.Context, profile *storage.OCIProfile, region, bucketName string) error {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return err
	}

	namespace, err := getNamespace(ctx, osClient)
	if err != nil {
		return err
	}

	_, err = osClient.DeleteBucket(ctx, objectstorage.DeleteBucketRequest{
		NamespaceName: common.String(namespace),
		BucketName:    common.String(bucketName),
	})
	return err
}
