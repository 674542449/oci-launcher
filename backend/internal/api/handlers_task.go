package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/engine"
	"oci-panel/internal/notify"
	"oci-panel/internal/oci"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CreateTaskRequest struct {
	ProfileID           uint     `json:"profile_id" binding:"required"`
	InstanceName        string   `json:"instance_name" binding:"omitempty,min=2,max=128"`
	Shape               string   `json:"shape" binding:"required,oneof=VM.Standard.A1.Flex VM.Standard.E2.1.Micro"`
	OCPU                float64  `json:"ocpu" binding:"required,min=1,max=4"`
	MemoryInGBs         float64  `json:"memory_in_gbs" binding:"required,min=1,max=24"`
	BootVolumeSizeInGBs int64    `json:"boot_volume_size_in_gbs" binding:"required,min=50,max=200"`
	BootVolumeVPU       int64    `json:"boot_volume_vpu" binding:"min=0,max=120"`
	Region              string   `json:"region" binding:"required"`
	ADList              []string `json:"ad_list"`
	ImageOCID           string   `json:"image_ocid" binding:"required"`
	SubnetOCID          string   `json:"subnet_ocid" binding:"required"`
	LoginMode           string   `json:"login_mode" binding:"omitempty,oneof=root_key root_password"`
	SSHAuthorizedKeys   string   `json:"ssh_authorized_keys"`
	RootPassword        string   `json:"root_password" binding:"max=128"`
	AssignPublicIP      bool     `json:"assign_public_ip"`
	EnableIPv6          bool     `json:"enable_ipv6"`
	RetryIntervalSecs   int      `json:"retry_interval_secs" binding:"min=0,max=86400"`
	MaxRetries          int      `json:"max_retries" binding:"min=0"`
}

const (
	accountLockHandlerTTL = 2 * time.Minute
	firstAttemptTimeout   = 150 * time.Second
)

func parseTaskID(c *gin.Context) (uuid.UUID, bool) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return uuid.Nil, false
	}
	return taskID, true
}

// acquireLockOrReject takes the single-account lock for the profile or writes a 409.
func acquireLockOrReject(c *gin.Context, profileID uint) bool {
	ok, lockedBy, err := cache.AcquireAccountLock(c.Request.Context(), profileID, accountLockHandlerTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取账号并发锁失败 (Redis 不可用): " + err.Error()})
		return false
	}
	if !ok {
		c.JSON(http.StatusConflict, gin.H{
			"error": "系统同一时间只允许操作一个 OCI 账号。账号 ID [" + lockedBy + "] 的任务正在运行，请先停止它或稍后再试。",
		})
		return false
	}
	return true
}

// ListTasks lists tasks, optionally filtered by profile
func ListTasks(c *gin.Context) {
	var tasks []storage.LaunchTask

	query := storage.DB.Order("created_at DESC")
	if raw := c.Query("profile_id"); raw != "" {
		profileID, ok := parseID(raw)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "profile_id 无效"})
			return
		}
		query = query.Where("profile_id = ?", profileID)
	}
	query.Find(&tasks)

	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// CreateTask creates the instance right away: one LaunchInstance attempt per availability
// domain, synchronously. On success the instance exists when the response returns. On a
// capacity-type failure the task is left "stopped" and the response says it is retryable, so
// the UI can ask the user whether to queue automatic retries (POST /tasks/start/:id).
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

	loginMode := req.LoginMode
	if loginMode == "" {
		loginMode = "root_key"
	}
	if loginMode == "root_password" && strings.TrimSpace(req.RootPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "密码登录模式需要提供 root 密码"})
		return
	}
	if strings.Contains(req.Shape, "Micro") {
		req.OCPU, req.MemoryInGBs = 1, 1
	}
	instanceName := strings.TrimSpace(req.InstanceName)
	if instanceName == "" {
		instanceName = engine.RandomInstanceName()
	}

	// One OCI account at a time
	if !acquireLockOrReject(c, profile.ID) {
		return
	}
	releaseLock := func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cache.ReleaseAccountLock(bg, profile.ID)
	}

	adList := ""
	if len(req.ADList) > 0 {
		adBytes, _ := json.Marshal(req.ADList)
		adList = string(adBytes)
	}

	vpu := req.BootVolumeVPU
	if vpu <= 0 {
		vpu = 120
	}

	// The root password is stored encrypted at rest and decrypted only inside the engine
	rootPasswordEnc := ""
	if loginMode == "root_password" {
		enc, err := security.EncryptAES256GCM(req.RootPassword, config.GlobalConfig.MasterKey)
		if err != nil {
			releaseLock()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加密 root 密码失败: " + err.Error()})
			return
		}
		rootPasswordEnc = enc
	}

	task := storage.LaunchTask{
		ID:                  uuid.New(),
		ProfileID:           profile.ID,
		InstanceName:        instanceName,
		Shape:               req.Shape,
		OCPU:                req.OCPU,
		MemoryInGBs:         req.MemoryInGBs,
		BootVolumeSizeInGBs: req.BootVolumeSizeInGBs,
		BootVolumeVPU:       vpu,
		Region:              strings.TrimSpace(req.Region),
		ADList:              adList,
		ImageOCID:           req.ImageOCID,
		SubnetOCID:          req.SubnetOCID,
		LoginMode:           loginMode,
		SSHAuthorizedKeys:   strings.TrimSpace(req.SSHAuthorizedKeys),
		RootPasswordEnc:     rootPasswordEnc,
		AssignPublicIP:      req.AssignPublicIP,
		EnableIPv6:          req.EnableIPv6,
		Status:              "creating",
		RetryIntervalSecs:   req.RetryIntervalSecs,
		MaxRetries:          req.MaxRetries,
		CreatedAt:           time.Now(),
	}

	// Zero-cost guard: home region, storage total, A1 / Micro allowance
	if err := oci.ValidateFreeTierConstraint(c.Request.Context(), &profile, &task); err != nil {
		releaseLock()
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := storage.DB.Create(&task).Error; err != nil {
		releaseLock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存创建任务失败: " + err.Error()})
		return
	}

	storage.LogAudit("CREATE_INSTANCE", profile.Name, c.ClientIP(), c.GetHeader("User-Agent"),
		fmt.Sprintf("%s %s %.0fC/%.0fG %dGB in %s", task.InstanceName, task.Shape, task.OCPU, task.MemoryInGBs, task.BootVolumeSizeInGBs, task.Region), "SUCCESS")

	// ---- Synchronous first attempt (detached from the HTTP request so a client disconnect
	//      cannot leave a half-recorded attempt) ----
	ctx, cancel := context.WithTimeout(context.Background(), firstAttemptTimeout)
	defer cancel()

	adNames, err := engine.ResolveADList(ctx, &profile, &task)
	if err != nil {
		releaseLock()
		msg := "无法获取可用区列表: " + err.Error()
		storage.DB.Model(&task).Updates(map[string]interface{}{"status": "failed", "last_message": msg})
		c.JSON(http.StatusOK, gin.H{
			"result": "failed", "retryable": false, "reason": msg, "task_id": task.ID.String(), "task": task,
		})
		return
	}

	rootPassword := req.RootPassword
	var last engine.AttemptResult
	attempts := 0
	for i, ad := range adNames {
		attempts = i + 1
		last = engine.AttemptLaunch(ctx, &profile, &task, ad, attempts, rootPassword)
		if last.Success || last.Category == engine.CategoryFatalError || last.Category == engine.CategoryCancelled {
			break
		}
	}

	if last.Success {
		msg := "实例已创建，正在等待网络就绪"
		if last.AlreadyExisted {
			msg = "云端已存在同名实例，视为创建成功"
		}
		storage.DB.Model(&task).Updates(map[string]interface{}{
			"status":                "success",
			"success_instance_ocid": last.InstanceOCID,
			"last_message":          msg,
		})
		releaseLock()

		// Wait for RUNNING + public IP in the background, then notify
		taskCopy := task
		profileCopy := profile
		go func() {
			bg, cancelBg := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancelBg()
			engine.FinalizeSuccess(bg, &profileCopy, &taskCopy, last, attempts, rootPassword)
		}()

		c.JSON(http.StatusOK, gin.H{
			"result":        "created",
			"message":       msg,
			"task_id":       task.ID.String(),
			"instance_ocid": last.InstanceOCID,
			"existed":       last.AlreadyExisted,
			"task":          task,
		})
		return
	}

	releaseLock()

	if last.Category == engine.CategoryFatalError {
		storage.DB.Model(&task).Updates(map[string]interface{}{"status": "failed", "last_message": "创建失败: " + last.Reason})
		notify.NotifyTaskFatalError(&task, &profile, last.Reason)
		c.JSON(http.StatusOK, gin.H{
			"result": "failed", "retryable": false, "reason": last.Reason, "attempts": attempts,
			"task_id": task.ID.String(), "task": task,
		})
		return
	}

	reason := last.Reason
	if last.Category == engine.CategoryCancelled {
		reason = "创建请求超时，请重试"
	}
	storage.DB.Model(&task).Updates(map[string]interface{}{"status": "stopped", "last_message": "创建失败: " + reason + "（未排队）"})
	c.JSON(http.StatusOK, gin.H{
		"result": "failed", "retryable": true, "reason": reason, "attempts": attempts,
		"task_id": task.ID.String(), "task": task,
	})
}

// StartExistingTask queues automatic retries (60-180 s random interval) for a task that
// failed to create or was stopped
func StartExistingTask(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	var task storage.LaunchTask
	if err := storage.DB.First(&task, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}
	if task.Status == "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该任务已经成功创建实例，请新建任务"})
		return
	}
	if task.Status == "running" && engine.IsTaskActive(taskID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务已在排队重试中"})
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, task.ProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	if !acquireLockOrReject(c, profile.ID) {
		return
	}

	if err := oci.ValidateFreeTierConstraint(c.Request.Context(), &profile, &task); err != nil {
		_ = cache.ReleaseAccountLock(c.Request.Context(), profile.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := engine.StartTask(taskID); err != nil {
		_ = cache.ReleaseAccountLock(c.Request.Context(), profile.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已加入排队，每 60-180 秒随机重试一次，直到创建成功"})
}

// StopExistingTask stops a queued task
func StopExistingTask(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	engine.StopTask(taskID)
	c.JSON(http.StatusOK, gin.H{"message": "已停止排队"})
}

// DeleteExistingTask deletes a task and its attempt history
func DeleteExistingTask(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	engine.StopTask(taskID)
	storage.DB.Where("task_id = ?", taskID).Delete(&storage.TaskAttempt{})
	storage.DB.Delete(&storage.LaunchTask{}, "id = ?", taskID)

	c.JSON(http.StatusOK, gin.H{"message": "记录已删除"})
}

// ListPresets returns pre-configured free-tier presets
func ListPresets(c *gin.Context) {
	var presets []storage.Preset
	storage.DB.Order("id ASC").Find(&presets)
	c.JSON(http.StatusOK, gin.H{"presets": presets})
}

// ListDynamicUbuntuImages returns the newest two official Ubuntu LTS images for a shape
func ListDynamicUbuntuImages(c *gin.Context) {
	profile, ok := profileFromQuery(c)
	if !ok {
		return
	}

	shape := c.Query("shape")
	if shape == "" {
		shape = "VM.Standard.A1.Flex"
	}
	region := c.Query("region")
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

// ListTaskAttempts returns the most recent attempts of a task
func ListTaskAttempts(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	var attempts []storage.TaskAttempt
	storage.DB.Where("task_id = ?", taskID).Order("attempt_num DESC").Limit(100).Find(&attempts)

	c.JSON(http.StatusOK, gin.H{"attempts": attempts})
}

// GetAuditLogs returns the most recent audit log entries
func GetAuditLogs(c *gin.Context) {
	var logs []storage.AuditLog
	storage.DB.Order("created_at DESC").Limit(200).Find(&logs)
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

var allowedSettingKeys = map[string]bool{
	"tg_bot_token": true,
	"tg_chat_id":   true,
}

// SaveSetting saves a system setting
func SaveSetting(c *gin.Context) {
	var req struct {
		Key   string `json:"key" binding:"required"`
		Value string `json:"value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if !allowedSettingKeys[req.Key] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的设置项: " + req.Key})
		return
	}

	setting := storage.SystemSetting{
		Key:       req.Key,
		Value:     strings.TrimSpace(req.Value),
		UpdatedAt: time.Now(),
	}
	storage.DB.Save(&setting)

	storage.LogAudit("SAVE_SETTING", "admin", c.ClientIP(), c.GetHeader("User-Agent"), "Updated "+req.Key, "SUCCESS")

	c.JSON(http.StatusOK, gin.H{"message": "设置已保存"})
}

// GetSettings returns all settings (bot token masked)
func GetSettings(c *gin.Context) {
	var settings []storage.SystemSetting
	storage.DB.Find(&settings)

	res := make(map[string]string)
	for _, s := range settings {
		val := s.Value
		if s.Key == "tg_bot_token" && len(val) > 8 {
			val = val[:4] + "********" + val[len(val)-4:]
		}
		res[s.Key] = val
	}

	c.JSON(http.StatusOK, gin.H{"settings": res})
}

// TestTelegram sends a test message with the saved bot token and chat id.
// It runs server-side because the browser CSP does not allow calls to api.telegram.org.
func TestTelegram(c *gin.Context) {
	botToken, chatID := notify.GetGlobalTelegramConfig()
	if botToken == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先保存 Bot Token 和 Chat ID"})
		return
	}
	text := "🔔 <b>OCI 控制台测试消息</b>\nTelegram 通知配置正常，实例创建成功与任务熔断告警会发送到这里。"
	if err := notify.SendTelegramMessage(botToken, chatID, text); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "发送失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试消息已发送，请查看 Telegram"})
}
