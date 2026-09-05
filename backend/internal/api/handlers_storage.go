package api

import (
	"net/http"

	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

type ResizeBootVolumeRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
	NewSizeGB int64  `json:"new_size_gb" binding:"required,min=47,max=200"`
	NewVPU    *int64 `json:"new_vpu"` // 0 to 120
}

type BackupBootVolumeRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
	Name      string `json:"name" binding:"required"`
}

type CreateBlockVolumeRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	AD        string `json:"ad" binding:"required"`
	Name      string `json:"name" binding:"required"`
	SizeGB    int64  `json:"size_gb" binding:"required,min=50,max=200"`
	VPU       int64  `json:"vpu" binding:"min=0,max=120"`
}

type BucketRequest struct {
	ProfileID  uint   `json:"profile_id" binding:"required"`
	Region     string `json:"region" binding:"required"`
	BucketName string `json:"bucket_name" binding:"required"`
}

// ListBootVolumes lists boot volumes with VPU and grow commands
func ListBootVolumes(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	items, err := oci.ListBootVolumes(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取引导卷失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"boot_volumes": items})
}

// ResizeBootVolume updates boot volume size and VPU (0-120)
func ResizeBootVolume(c *gin.Context) {
	var req ResizeBootVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	vpu := int64(120)
	if req.NewVPU != nil {
		vpu = *req.NewVPU
	}
	if vpu < 10 {
		vpu = 10 // OCI Boot Volumes strictly require minimum 10 VPU
	}
	if vpu > 120 {
		vpu = 120 // Maximum 120 VPU Ultra High Performance
	}

	err := oci.ResizeBootVolume(c.Request.Context(), &profile, req.Region, req.OCID, req.NewSizeGB, vpu)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "扩容或调整性能失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "引导卷扩容与性能调整指令已下发，请在虚拟机内执行扩容命令"})
}

// CreateBootVolumeBackup creates a backup
func CreateBootVolumeBackup(c *gin.Context) {
	var req BackupBootVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.CreateBootVolumeBackup(c.Request.Context(), &profile, req.Region, req.OCID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建备份失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "引导卷快照备份已开始创建"})
}

// ListBlockVolumes lists block volumes
func ListBlockVolumes(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	items, err := oci.ListBlockVolumes(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取块存储卷失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"block_volumes": items})
}

// CreateBlockVolume creates a block volume
func CreateBlockVolume(c *gin.Context) {
	var req CreateBlockVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	vpu := req.VPU
	if vpu <= 0 {
		vpu = 120
	}

	err := oci.CreateBlockVolume(c.Request.Context(), &profile, req.Region, req.AD, req.Name, req.SizeGB, vpu)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建块存储失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "块存储创建成功"})
}

// ListBuckets lists buckets
func ListBuckets(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	buckets, err := oci.ListBuckets(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取存储桶失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"buckets": buckets})
}

// CreateBucket creates bucket
func CreateBucket(c *gin.Context) {
	var req BucketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.CreateBucket(c.Request.Context(), &profile, req.Region, req.BucketName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建存储桶失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "存储桶创建成功"})
}

// DeleteBucket deletes bucket
func DeleteBucket(c *gin.Context) {
	var req BucketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.DeleteBucket(c.Request.Context(), &profile, req.Region, req.BucketName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除存储桶失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "存储桶已删除"})
}
