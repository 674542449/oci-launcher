package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/config"
	"oci-panel/internal/notify"
	"oci-panel/internal/oci"
	"oci-panel/internal/security"
	"oci-panel/internal/storage"

	"github.com/google/uuid"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type LiveLogMessage struct {
	TaskID     string `json:"task_id"`
	AttemptNum int    `json:"attempt_num"`
	Timestamp  string `json:"timestamp"` // RFC3339 (UTC); the UI renders it in local time
	Region     string `json:"region"`
	AD         string `json:"ad"`
	Status     string `json:"status"` // SUCCESS, RETRY, RATE_LIMIT, FATAL, STOPPED, WAIT
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

const (
	maxBackoffFactor      = 16
	instanceReadyTimeout  = 5 * time.Minute
	instanceReadyInterval = 10 * time.Second
)

// ParseADList tolerates the JSON array stored by the API as well as legacy comma-separated
// values; "null", "[]" and blanks mean "no preference" (rotate through every AD of the region).
func ParseADList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		list = strings.Split(raw, ",")
	}
	out := list[:0]
	for _, ad := range list {
		ad = strings.TrimSpace(ad)
		if ad != "" && ad != "null" {
			out = append(out, ad)
		}
	}
	return out
}

// DecryptTaskRootPassword returns the plaintext root password of a task. Older rows may still
// hold the raw value, which is returned unchanged when decryption fails.
func DecryptTaskRootPassword(enc string) string {
	if enc == "" {
		return ""
	}
	if plain, err := security.DecryptAES256GCM(enc, config.GlobalConfig.MasterKey); err == nil {
		return plain
	}
	return enc
}

// sleepCtx waits for d unless the context is cancelled first (returns false in that case).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func lockTTL(intervalSecs int) time.Duration {
	ttl := time.Duration(intervalSecs)*2*time.Second + 60*time.Second
	if ttl < 2*time.Minute {
		ttl = 2 * time.Minute
	}
	return ttl
}

func recordAttempt(taskID uuid.UUID, num int, region, ad, status, msg string, durationMs int64) {
	storage.DB.Create(&storage.TaskAttempt{
		TaskID:          taskID,
		AttemptNum:      num,
		Region:          region,
		AD:              ad,
		Status:          status,
		ResponseMessage: msg,
		DurationMs:      durationMs,
		CreatedAt:       time.Now(),
	})
}

func setTaskStatus(taskID uuid.UUID, status, message string) {
	storage.DB.Model(&storage.LaunchTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"status":       status,
		"last_message": message,
	})
}

// waitForInstanceAddresses polls until the instance is RUNNING and its primary VNIC has the
// requested public IP (bounded by instanceReadyTimeout).
func waitForInstanceAddresses(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, wantPublicIP bool) (state, pubIP, ipv6 string) {
	deadline := time.Now().Add(instanceReadyTimeout)
	for {
		s, ip, v6, err := oci.GetInstanceAddresses(ctx, profile, region, instanceOCID)
		if err == nil {
			state, pubIP, ipv6 = s, ip, v6
			if state == string(core.InstanceLifecycleStateRunning) && (pubIP != "" || !wantPublicIP) {
				return
			}
			if state == string(core.InstanceLifecycleStateTerminated) || state == string(core.InstanceLifecycleStateTerminating) {
				return
			}
		}
		if time.Now().After(deadline) || !sleepCtx(ctx, instanceReadyInterval) {
			return
		}
	}
}

// RunTaskWorker runs the retry loop of one launch task until it succeeds, fails or is stopped.
func RunTaskWorker(ctx context.Context, taskID uuid.UUID) {
	log.Printf("[Engine] Starting worker for task: %s", taskID)

	var task storage.LaunchTask
	if err := storage.DB.First(&task, "id = ?", taskID).Error; err != nil {
		log.Printf("[Engine] Task %s not found in database", taskID)
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, "id = ?", task.ProfileID).Error; err != nil {
		log.Printf("[Engine] Profile %d for task %s not found", task.ProfileID, taskID)
		setTaskStatus(taskID, "failed", "关联的 OCI 账号不存在")
		return
	}

	// Hand the global account lock back when this is the last running task of the profile.
	defer func() {
		var others int64
		storage.DB.Model(&storage.LaunchTask{}).
			Where("profile_id = ? AND status = ? AND id <> ?", profile.ID, "running", taskID).
			Count(&others)
		if others == 0 {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = cache.ReleaseAccountLock(bg, profile.ID)
		}
	}()

	// Availability domains: user selection, otherwise every AD of the target region
	adList := ParseADList(task.ADList)
	if len(adList) == 0 {
		names, err := oci.ListAvailabilityDomainNames(ctx, &profile, task.Region)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			setTaskStatus(taskID, "failed", "无法获取可用区列表: "+err.Error())
			return
		}
		adList = names
	}
	if len(adList) == 0 {
		setTaskStatus(taskID, "failed", "未找到可用区 (AD)，请检查区域配置")
		return
	}

	rootPassword := DecryptTaskRootPassword(task.RootPasswordEnc)

	currentADIndex := 0
	backoffFactor := 1

	for {
		if ctx.Err() != nil {
			log.Printf("[Engine] Task %s worker stopped via context", taskID)
			return
		}

		// Re-read task and profile every iteration: the user may stop the task, edit or delete the account.
		var currentTask storage.LaunchTask
		if err := storage.DB.First(&currentTask, "id = ?", taskID).Error; err != nil || currentTask.Status != "running" {
			log.Printf("[Engine] Task %s is no longer running (%s), exiting loop", taskID, currentTask.Status)
			return
		}
		if err := storage.DB.First(&profile, "id = ?", task.ProfileID).Error; err != nil {
			setTaskStatus(taskID, "failed", "关联的 OCI 账号已被删除，任务停止")
			return
		}

		interval := currentTask.RetryIntervalSecs
		if interval <= 0 {
			interval = 60
		}

		// One OCI account operated at a time: take (or refresh) the global lock
		if ok, lockedBy, err := cache.AcquireAccountLock(ctx, profile.ID, lockTTL(interval)); err != nil {
			log.Printf("[Engine] Task %s: account lock error: %v", taskID, err)
		} else if !ok {
			emitLog(ctx, taskID.String(), currentTask.CurrentRetries, task.Region, "", "WAIT",
				fmt.Sprintf("等待账号并发锁：账号 ID %s 正在操作中，%d 秒后再试", lockedBy, interval), 0)
			if !sleepCtx(ctx, time.Duration(interval)*time.Second) {
				return
			}
			continue
		}

		// Idempotency: an instance with the same name already alive means we are done.
		if computeClient, err := oci.GetComputeClient(&profile, task.Region); err == nil {
			instList, err2 := computeClient.ListInstances(ctx, core.ListInstancesRequest{
				CompartmentId: common.String(profile.TenancyOCID),
				DisplayName:   common.String(task.InstanceName),
			})
			if ctx.Err() != nil {
				return
			}
			if err2 == nil {
				for _, inst := range instList.Items {
					if inst.LifecycleState == core.InstanceLifecycleStateTerminated || inst.LifecycleState == core.InstanceLifecycleStateTerminating {
						continue
					}
					instOCID := oci.StrVal(inst.Id)
					log.Printf("[Engine] Found existing instance %s (%s), marking task success", task.InstanceName, instOCID)
					_, pubIP, ipv6 := waitForInstanceAddresses(ctx, &profile, task.Region, instOCID, currentTask.AssignPublicIP)
					if ctx.Err() != nil {
						return
					}
					storage.DB.Model(&currentTask).Updates(map[string]interface{}{
						"status":                "success",
						"success_instance_ocid": instOCID,
						"success_public_ip":     pubIP,
						"success_ipv6":          ipv6,
						"last_message":          "开机成功（检测到云端已存在同名实例，防重复生效）",
					})
					notify.NotifyTaskSuccess(&currentTask, &profile, pubIP, ipv6, rootPassword)
					emitLog(ctx, taskID.String(), currentTask.CurrentRetries+1, task.Region, "", "SUCCESS",
						fmt.Sprintf("开机成功：云端已存在同名实例 %s，公网 IP %s", instOCID, pubIP), 0)
					return
				}
			}
		}

		targetAD := adList[currentADIndex%len(adList)]
		currentADIndex++
		currentRetries := currentTask.CurrentRetries + 1

		// Launch (the task copy carries the decrypted password for cloud-init / tags)
		launchTask := currentTask
		launchTask.RootPasswordEnc = rootPassword
		startTime := time.Now()
		instanceOCID, launchErr := oci.LaunchInstance(ctx, &profile, &launchTask, targetAD)
		durationMs := time.Since(startTime).Milliseconds()
		if ctx.Err() != nil {
			// Stopped while the call was in flight: do not record a failure.
			return
		}

		now := time.Now()
		storage.DB.Model(&currentTask).Updates(map[string]interface{}{
			"current_retries": currentRetries,
			"last_attempt_at": &now,
		})

		if launchErr == nil && instanceOCID != "" {
			log.Printf("[Engine] Task %s launched instance %s", taskID, instanceOCID)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "SUCCESS",
				fmt.Sprintf("抢机成功！实例 %s 创建中，正在等待网络就绪…", instanceOCID), durationMs)

			state, pubIP, ipv6 := waitForInstanceAddresses(ctx, &profile, task.Region, instanceOCID, currentTask.AssignPublicIP)
			msg := "开机成功"
			if pubIP != "" {
				msg = "开机成功，公网 IP " + pubIP
			} else if state != "" {
				msg = fmt.Sprintf("开机成功，实例状态 %s，公网 IP 尚未就绪，请稍后在实例页刷新", state)
			}

			storage.DB.Model(&currentTask).Updates(map[string]interface{}{
				"status":                "success",
				"success_instance_ocid": instanceOCID,
				"success_public_ip":     pubIP,
				"success_ipv6":          ipv6,
				"last_message":          msg,
			})
			recordAttempt(taskID, currentRetries, task.Region, targetAD, "success", "创建实例成功 (Instance Launched)", durationMs)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "SUCCESS",
				fmt.Sprintf("%s。实例 OCID: %s", msg, instanceOCID), durationMs)
			notify.NotifyTaskSuccess(&currentTask, &profile, pubIP, ipv6, rootPassword)
			return
		}

		category, reason := ClassifyError(launchErr)
		var waitSecs int
		switch category {
		case CategoryCancelled:
			return

		case CategoryFatalError:
			log.Printf("[Engine] Task %s fatal error: %v", taskID, launchErr)
			setTaskStatus(taskID, "failed", "致命错误熔断: "+reason)
			recordAttempt(taskID, currentRetries, task.Region, targetAD, "fatal_error", reason, durationMs)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "FATAL", "配置或凭据错误，任务停止: "+reason, durationMs)
			notify.NotifyTaskFatalError(&currentTask, &profile, reason)
			return

		case CategoryRateLimited:
			backoffFactor *= 2
			if backoffFactor > maxBackoffFactor {
				backoffFactor = maxBackoffFactor
			}
			waitSecs = interval * backoffFactor
			recordAttempt(taskID, currentRetries, task.Region, targetAD, "rate_limited", reason, durationMs)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "RATE_LIMIT",
				fmt.Sprintf("429 限流，退避 %d 秒后重试", waitSecs), durationMs)

		case CategoryTransient:
			backoffFactor = 1
			waitSecs = interval + rand.Intn(10)
			recordAttempt(taskID, currentRetries, task.Region, targetAD, "transient_error", reason, durationMs)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "RETRY",
				fmt.Sprintf("%s，%d 秒后重试", reason, waitSecs), durationMs)

		default: // CategoryCapacityFull
			backoffFactor = 1
			waitSecs = interval + rand.Intn(10)
			recordAttempt(taskID, currentRetries, task.Region, targetAD, "capacity_full", reason, durationMs)
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "RETRY",
				fmt.Sprintf("容量不足 (Out of host capacity)，%d 秒后在下一可用区重试", waitSecs), durationMs)
		}

		// Attempt budget is checked before sleeping so the last attempt does not waste an interval
		if currentTask.MaxRetries > 0 && currentRetries >= currentTask.MaxRetries {
			setTaskStatus(taskID, "stopped", "已达到最大尝试次数限制，任务自动停止")
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "STOPPED", "已达到设定的最大重试次数，任务停止", durationMs)
			return
		}

		if !sleepCtx(ctx, time.Duration(waitSecs)*time.Second) {
			return
		}
	}
}

func emitLog(ctx context.Context, taskID string, attemptNum int, region, ad, status, message string, durationMs int64) {
	logMsg := LiveLogMessage{
		TaskID:     taskID,
		AttemptNum: attemptNum,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Region:     region,
		AD:         ad,
		Status:     status,
		Message:    message,
		DurationMs: durationMs,
	}

	payload, err := json.Marshal(logMsg)
	if err != nil {
		return
	}
	// Publishing must work even while the worker context is being cancelled
	bg, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = ctx
	_ = cache.PublishTaskLog(bg, taskID, string(payload))
}
