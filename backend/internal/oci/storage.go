package oci

import (
	"context"
	"fmt"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
)

type BootVolumeItem struct {
	OCID         string `json:"ocid"`
	DisplayName  string `json:"display_name"`
	SizeInGBs    int64  `json:"size_in_gbs"`
	VpusPerGB    int64  `json:"vpus_per_gb"` // 10 to 120 (Boot volumes require min 10 VPU)
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
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	StorageTier   string `json:"storage_tier"`
	PublicAccess  string `json:"public_access"`
	ApproxSizeGB  int64  `json:"approx_size_gb"`
	TimeCreated   string `json:"time_created"`
}

// ListBootVolumes lists boot volumes across all availability domains
func ListBootVolumes(ctx context.Context, profile *storage.OCIProfile, region string) ([]BootVolumeItem, error) {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	idClient, err := GetIdentityClient(profile)
	if err != nil {
		return nil, err
	}

	adResp, err := idClient.ListAvailabilityDomains(ctx, identity.ListAvailabilityDomainsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list availability domains: %w", err)
	}

	var items []BootVolumeItem
	for _, ad := range adResp.Items {
		if ad.Name == nil {
			continue
		}
		req := core.ListBootVolumesRequest{
			AvailabilityDomain: ad.Name,
			CompartmentId:      common.String(profile.TenancyOCID),
		}

		resp, err := blockClient.ListBootVolumes(ctx, req)
		if err != nil {
			continue
		}

		for _, bv := range resp.Items {
			if bv.LifecycleState == core.BootVolumeLifecycleStateTerminated {
				continue
			}

			size := int64(50)
			if bv.SizeInGBs != nil {
				size = *bv.SizeInGBs
			}
			vpu := int64(120)
			if bv.VpusPerGB != nil {
				vpu = *bv.VpusPerGB
			}

			timeStr := ""
			if bv.TimeCreated != nil {
				timeStr = bv.TimeCreated.Format("2006-01-02 15:04:05")
			}

			growCmds := fmt.Sprintf("# 适用于 Oracle Linux / Ubuntu (一键自动扩展磁盘分区):\nsudo oci-growfs -y || (sudo apt-get install -y cloud-guest-utils && sudo growpart /dev/sda 1 && sudo resize2fs /dev/sda1)")

			items = append(items, BootVolumeItem{
				OCID:         StrVal(bv.Id),
				DisplayName:  StrVal(bv.DisplayName),
				SizeInGBs:    size,
				VpusPerGB:    vpu,
				State:        string(bv.LifecycleState),
				AD:           StrVal(bv.AvailabilityDomain),
				TimeCreated:  timeStr,
				GrowCommands: growCmds,
			})
		}
	}

	return items, nil
}

// ResizeBootVolume updates boot volume size and VPU (10-120)
func ResizeBootVolume(ctx context.Context, profile *storage.OCIProfile, region, bootVolumeOCID string, newSizeGB, newVPU int64) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	req := core.UpdateBootVolumeRequest{
		BootVolumeId: common.String(bootVolumeOCID),
		UpdateBootVolumeDetails: core.UpdateBootVolumeDetails{
			SizeInGBs:   common.Int64(newSizeGB),
			VpusPerGB:   common.Int64(newVPU),
		},
	}

	_, err = blockClient.UpdateBootVolume(ctx, req)
	return err
}

// CreateBootVolumeBackup creates a backup snapshot
func CreateBootVolumeBackup(ctx context.Context, profile *storage.OCIProfile, region, bootVolumeOCID, name string) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	req := core.CreateBootVolumeBackupRequest{
		CreateBootVolumeBackupDetails: core.CreateBootVolumeBackupDetails{
			BootVolumeId: common.String(bootVolumeOCID),
			DisplayName:  common.String(name),
			Type:         core.CreateBootVolumeBackupDetailsTypeFull,
		},
	}

	_, err = blockClient.CreateBootVolumeBackup(ctx, req)
	return err
}

// ListBlockVolumes lists block volumes
func ListBlockVolumes(ctx context.Context, profile *storage.OCIProfile, region string) ([]BlockVolumeItem, error) {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListVolumesRequest{
		CompartmentId: common.String(profile.TenancyOCID),
	}

	resp, err := blockClient.ListVolumes(ctx, req)
	if err != nil {
		return nil, err
	}

	var items []BlockVolumeItem
	for _, vol := range resp.Items {
		if vol.LifecycleState == core.VolumeLifecycleStateTerminated {
			continue
		}

		size := int64(50)
		if vol.SizeInGBs != nil {
			size = *vol.SizeInGBs
		}
		vpu := int64(120)
		if vol.VpusPerGB != nil {
			vpu = *vol.VpusPerGB
		}

		timeStr := ""
		if vol.TimeCreated != nil {
			timeStr = vol.TimeCreated.Format("2006-01-02 15:04:05")
		}

		items = append(items, BlockVolumeItem{
			OCID:        StrVal(vol.Id),
			DisplayName: StrVal(vol.DisplayName),
			SizeInGBs:   size,
			VpusPerGB:   vpu,
			State:       string(vol.LifecycleState),
			AD:          StrVal(vol.AvailabilityDomain),
			TimeCreated: timeStr,
		})
	}

	return items, nil
}

// CreateBlockVolume creates a new block volume with VPU
func CreateBlockVolume(ctx context.Context, profile *storage.OCIProfile, region, ad, name string, sizeGB, vpu int64) error {
	blockClient, err := GetBlockstorageClient(profile, region)
	if err != nil {
		return err
	}

	req := core.CreateVolumeRequest{
		CreateVolumeDetails: core.CreateVolumeDetails{
			CompartmentId:      common.String(profile.TenancyOCID),
			AvailabilityDomain: common.String(ad),
			DisplayName:        common.String(name),
			SizeInGBs:          common.Int64(sizeGB),
			VpusPerGB:          common.Int64(vpu),
		},
	}

	_, err = blockClient.CreateVolume(ctx, req)
	return err
}

// ListBuckets lists Object Storage buckets
func ListBuckets(ctx context.Context, profile *storage.OCIProfile, region string) ([]BucketItem, error) {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return nil, err
	}

	// 1. Get Namespace
	nsResp, err := osClient.GetNamespace(ctx, objectstorage.GetNamespaceRequest{})
	if err != nil || nsResp.Value == nil {
		return nil, fmt.Errorf("failed to get object storage namespace: %w", err)
	}
	namespace := *nsResp.Value

	// 2. List buckets
	req := objectstorage.ListBucketsRequest{
		NamespaceName: common.String(namespace),
		CompartmentId: common.String(profile.TenancyOCID),
	}

	resp, err := osClient.ListBuckets(ctx, req)
	if err != nil {
		return nil, err
	}

	var items []BucketItem
	for _, b := range resp.Items {
		timeStr := ""
		if b.TimeCreated != nil {
			timeStr = b.TimeCreated.Format("2006-01-02 15:04:05")
		}
		items = append(items, BucketItem{
			Name:        StrVal(b.Name),
			Namespace:   namespace,
			TimeCreated: timeStr,
		})
	}

	return items, nil
}

// CreateBucket creates an Object Storage bucket
func CreateBucket(ctx context.Context, profile *storage.OCIProfile, region, bucketName string) error {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return err
	}

	nsResp, err := osClient.GetNamespace(ctx, objectstorage.GetNamespaceRequest{})
	if err != nil || nsResp.Value == nil {
		return err
	}

	req := objectstorage.CreateBucketRequest{
		NamespaceName: nsResp.Value,
		CreateBucketDetails: objectstorage.CreateBucketDetails{
			Name:          common.String(bucketName),
			CompartmentId: common.String(profile.TenancyOCID),
			PublicAccessType: objectstorage.CreateBucketDetailsPublicAccessTypeNopublicaccess,
			StorageTier:   objectstorage.CreateBucketDetailsStorageTierStandard,
		},
	}

	_, err = osClient.CreateBucket(ctx, req)
	return err
}

// DeleteBucket deletes an Object Storage bucket
func DeleteBucket(ctx context.Context, profile *storage.OCIProfile, region, bucketName string) error {
	osClient, err := GetObjectStorageClient(profile, region)
	if err != nil {
		return err
	}

	nsResp, err := osClient.GetNamespace(ctx, objectstorage.GetNamespaceRequest{})
	if err != nil || nsResp.Value == nil {
		return err
	}

	req := objectstorage.DeleteBucketRequest{
		NamespaceName: nsResp.Value,
		BucketName:    common.String(bucketName),
	}

	_, err = osClient.DeleteBucket(ctx, req)
	return err
}
