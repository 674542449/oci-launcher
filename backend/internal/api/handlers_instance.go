package api

import (
	"fmt"
	"net/http"
	"time"

	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
)

type InstanceActionRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
	Action    string `json:"action" binding:"required"` // START, STOP, SOFTRESET, RESET
}

type ResizeInstanceRequest struct {
	ProfileID uint    `json:"profile_id" binding:"required"`
	Region    string  `json:"region" binding:"required"`
	OCID      string  `json:"ocid" binding:"required"`
	NewOCPU   float32 `json:"new_ocpu" binding:"required,min=1,max=4"`
	NewMemory float32 `json:"new_memory" binding:"required,min=1,max=24"`
}

type RotateIPRequest struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
}

type ProbeIPRequest struct {
	IP   string `json:"ip" binding:"required"`
	Port int    `json:"port"`
}

type AttachIPv6Request struct {
	ProfileID uint   `json:"profile_id" binding:"required"`
	Region    string `json:"region" binding:"required"`
	OCID      string `json:"ocid" binding:"required"`
}

type UpdateTagsRequest struct {
	ProfileID uint              `json:"profile_id" binding:"required"`
	Region    string            `json:"region" binding:"required"`
	OCID      string            `json:"ocid" binding:"required"`
	Tags      map[string]string `json:"tags" binding:"required"`
}

// ListInstances lists all compute instances with VNIC details and root password tag
func ListInstances(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	instances, err := oci.ListInstancesWithDetails(c.Request.Context(), &profile, profile.Region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取实例列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"instances": instances,
	})
}

// PerformInstanceAction starts, stops, or restarts an instance
func PerformInstanceAction(c *gin.Context) {
	var req InstanceActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.InstanceAction(c.Request.Context(), &profile, req.Region, req.OCID, req.Action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "操作失败: " + err.Error()})
		return
	}

	storage.LogAudit("INSTANCE_ACTION", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf("Action %s on %s", req.Action, req.OCID), "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("操作 [%s] 指令已成功下发至云端", req.Action)})
}

// TerminateInstance deletes an instance
func TerminateInstance(c *gin.Context) {
	var req InstanceActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.TerminateInstance(c.Request.Context(), &profile, req.Region, req.OCID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "终止实例失败: " + err.Error()})
		return
	}

	storage.LogAudit("INSTANCE_TERMINATE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"), fmt.Sprintf("Terminated %s", req.OCID), "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": "实例终止（销毁）指令已成功执行"})
}

// ResizeInstance modifies CPU and memory of an instance
func ResizeInstance(c *gin.Context) {
	var req ResizeInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.ResizeInstance(c.Request.Context(), &profile, req.Region, req.OCID, req.NewOCPU, req.NewMemory)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "改配失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "改配指令已成功执行"})
}

// RotatePublicIP generates and binds a new ephemeral public IP
func RotatePublicIP(c *gin.Context) {
	var req RotateIPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	newIP, err := oci.RotatePublicIP(c.Request.Context(), &profile, req.Region, req.OCID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更换公网 IP 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "公网 IP 已成功更换！",
		"new_ip":  newIP,
	})
}

// ProbeIP tests TCP connection on port (default 22)
func ProbeIP(c *gin.Context) {
	var req ProbeIPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IP required"})
		return
	}

	port := req.Port
	if port <= 0 {
		port = 22
	}

	reachable := oci.ProbeIPPort(req.IP, port, 3*time.Second)

	c.JSON(http.StatusOK, gin.H{
		"ip":        req.IP,
		"port":      port,
		"reachable": reachable,
		"status":    fmt.Sprintf("端口 %d 连通性测试: %v", port, reachable),
	})
}

// AttachIPv6 attaches a new IPv6 address to an existing instance
func AttachIPv6(c *gin.Context) {
	var req AttachIPv6Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	ipv6, err := oci.AttachIPv6ToInstance(c.Request.Context(), &profile, req.Region, req.OCID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "附加 IPv6 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "IPv6 已成功分配并绑定！",
		"ipv6":    ipv6,
	})
}

// UpdateInstanceTags updates instance freeform tags (allowing root_password editing)
func UpdateInstanceTags(c *gin.Context) {
	var req UpdateTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	err := oci.UpdateInstanceTags(c.Request.Context(), &profile, req.Region, req.OCID, req.Tags)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新标签失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "实例云端标签已成功更新！"})
}
