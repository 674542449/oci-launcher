package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/engine"
	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CreateTaskRequest struct {
	ProfileID           uint     `json:"profile_id" binding:"required"`
	InstanceName        string   `json:"instance_name" binding:"required,min=2,max=128"`
	Shape               string   `json:"shape" binding:"required"`
	OCPU                float64  `json:"ocpu" binding:"required,min=1,max=4"`
	MemoryInGBs         float64  `json:"memory_in_gbs" binding:"required,min=1,max=24"`
	BootVolumeSizeInGBs int64    `json:"boot_volume_size_in_gbs" binding:"required,min=47,max=200"`
	BootVolumeVPU       int64    `json:"boot_volume_vpu"` // Default 120 VPU Ultra High Performance
	Region              string   `json:"region" binding:"required"`
	ADList              []string `json:"ad_list"`
	ImageOCID           string   `json:"image_ocid" binding:"required"`
	SubnetOCID          string   `json:"subnet_ocid" binding:"required"`
	LoginMode           string   `json:"login_mode"` // root_key, root_password
	SSHAuthorizedKeys   string   `json:"ssh_authorized_keys"`
	RootPassword        string   `json:"root_password"`
	AssignPublicIP      bool     `json:"assign_public_ip"`
	EnableIPv6          bool     `json:"enable_ipv6"`
	RetryIntervalSecs   int      `json:"retry_interval_secs"`
	MaxRetries          int      `json:"max_retries"`
}

// ListTasks lists all tasks
func ListTasks(c *gin.Context) {
	profileID := c.Query("profile_id")
	var tasks []storage.LaunchTask

	query := storage.DB.Order("created_at DESC")
	if profileID != "" {
		query = query.Where("profile_id = ?", profileID)
	}
	query.Find(&tasks)

	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// CreateTask creates a launch task and starts background worker
func CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入参校验未通过: " + err.Error()})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, req.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OCI 账号 Profile 不存在"})
		return
	}

	// 1. Single-Account Concurrency Lock check: only 1 profile can be operated at the same time
	ok, lockedBy, err := cache.AcquireAccountLock(c.Request.Context(), profile.ID, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取账号并发锁失败 (Redis 不可用): " + err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusConflict, gin.H{
			"error": "【单账号并发锁限制】系统同一时间最多只允许并发操作一个 OCI 账号。当前正在执行账号 ID [" + lockedBy + "] 的操作，系统保护生效中，请稍候再试。",
		})
		return
	}

	// 2. Format AD list as JSON
	adBytes, _ := json.Marshal(req.ADList)

	vpu := req.BootVolumeVPU
	if vpu <= 0 {
		vpu = 120 // Default 120 VPU
	}

	loginMode := req.LoginMode
	if loginMode == "" {
		loginMode = "root_key"
	}

	retryInterval := req.RetryIntervalSecs
	if retryInterval <= 0 {
		retryInterval = 60
	}

	task := storage.LaunchTask{
		ID:                  uuid.New(),
		ProfileID:           profile.ID,
		InstanceName:        req.InstanceName,
		Shape:               req.Shape,
		OCPU:                req.OCPU,
		MemoryInGBs:         req.MemoryInGBs,
		BootVolumeSizeInGBs: req.BootVolumeSizeInGBs,
		BootVolumeVPU:       vpu,
		Region:              req.Region,
		ADList:              string(adBytes),
		ImageOCID:           req.ImageOCID,
		SubnetOCID:          req.SubnetOCID,
		LoginMode:           loginMode,
		SSHAuthorizedKeys:   req.SSHAuthorizedKeys,
		RootPasswordEnc:     req.RootPassword,
		AssignPublicIP:      req.AssignPublicIP,
		EnableIPv6:          req.EnableIPv6,
		Status:              "running",
		RetryIntervalSecs:   retryInterval,
		MaxRetries:          req.MaxRetries,
		CreatedAt:           time.Now(),
	}

	// 3. 100% Free Tier Boundary Hard-Blocking Check!
	if err := oci.ValidateFreeTierConstraint(c.Request.Context(), &profile, &task); err != nil {
		_ = cache.ReleaseAccountLock(c.Request.Context(), profile.ID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 4. Save to DB
	if err := storage.DB.Create(&task).Error; err != nil {
		_ = cache.ReleaseAccountLock(c.Request.Context(), profile.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存开机任务失败: " + err.Error()})
		return
	}

	// 5. Start async retry engine
	if err := engine.StartTask(task.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "启动后台抢机调度器失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "开机任务已成功创建并在后台全速启动！",
		"task_id": task.ID.String(),
		"task":    task,
	})
}

// StartExistingTask resumes a stopped task
func StartExistingTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	var task storage.LaunchTask
	if err := storage.DB.First(&task, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, task.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	// Free tier re-validation
	if err := oci.ValidateFreeTierConstraint(c.Request.Context(), &profile, &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := engine.StartTask(taskID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "抢机任务已重新启动"})
}

// StopExistingTask pauses a running task
func StopExistingTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	engine.StopTask(taskID)
	c.JSON(http.StatusOK, gin.H{"message": "抢机任务已停止"})
}

// DeleteExistingTask deletes a task and attempt history
func DeleteExistingTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	engine.StopTask(taskID)
	storage.DB.Where("task_id = ?", taskID).Delete(&storage.TaskAttempt{})
	storage.DB.Delete(&storage.LaunchTask{}, "id = ?", taskID)

	c.JSON(http.StatusOK, gin.H{"message": "任务及历史记录已删除"})
}

// ListPresets returns pre-configured free-tier presets
func ListPresets(c *gin.Context) {
	var presets []storage.Preset
	storage.DB.Order("id ASC").Find(&presets)
	c.JSON(http.StatusOK, gin.H{"presets": presets})
}

// ListDynamicUbuntuImages queries latest 2 official Ubuntu LTS images
func ListDynamicUbuntuImages(c *gin.Context) {
	profileID := c.Query("profile_id")
	shape := c.Query("shape")
	region := c.Query("region")

	if shape == "" {
		shape = "VM.Standard.A1.Flex"
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	if region == "" {
		region = profile.Region
	}

	images, err := oci.GetTop2UbuntuImages(c.Request.Context(), &profile, shape, region)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"images": images})
}

// ListTaskAttempts returns attempts for a task
func ListTaskAttempts(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	var attempts []storage.TaskAttempt
	storage.DB.Where("task_id = ?", taskID).Order("attempt_num DESC").Limit(100).Find(&attempts)

	c.JSON(http.StatusOK, gin.H{"attempts": attempts})
}

// GetAuditLogs returns immutable audit logs
func GetAuditLogs(c *gin.Context) {
	var logs []storage.AuditLog
	storage.DB.Order("created_at DESC").Limit(200).Find(&logs)
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// SaveSetting saves Telegram or system setting
func SaveSetting(c *gin.Context) {
	var req struct {
		Key   string `json:"key" binding:"required"`
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	setting := storage.SystemSetting{
		Key:       req.Key,
		Value:     strings.TrimSpace(req.Value),
		UpdatedAt: time.Now(),
	}
	storage.DB.Save(&setting)

	c.JSON(http.StatusOK, gin.H{"message": "设置已保存"})
}

// GetSettings gets all settings
func GetSettings(c *gin.Context) {
	var settings []storage.SystemSetting
	storage.DB.Find(&settings)

	res := make(map[string]string)
	for _, s := range settings {
		// Mask sensitive bot token
		val := s.Value
		if s.Key == "tg_bot_token" && len(val) > 8 {
			val = val[:4] + "********" + val[len(val)-4:]
		}
		res[s.Key] = val
	}

	c.JSON(http.StatusOK, gin.H{"settings": res})
}
