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
	"oci-panel/internal/notify"
	"oci-panel/internal/oci"
	"oci-panel/internal/storage"

	"github.com/google/uuid"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

type LiveLogMessage struct {
	TaskID     string `json:"task_id"`
	AttemptNum int    `json:"attempt_num"`
	Timestamp  string `json:"timestamp"`
	Region     string `json:"region"`
	AD         string `json:"ad"`
	Status     string `json:"status"` // SUCCESS, RETRY, RATE_LIMIT, FATAL
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

// RunTaskWorker runs the asynchronous retry loop for a launch task
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
		return
	}

	// Parse AD List
	var adList []string
	if err := json.Unmarshal([]byte(task.ADList), &adList); err != nil || len(adList) == 0 {
		// Fallback: try comma separated or single AD
		if task.ADList != "" {
			adList = strings.Split(task.ADList, ",")
		}
	}
	if len(adList) == 0 {
		// Fetch ADs dynamically from OCI
		idClient, err := oci.GetIdentityClient(&profile)
		if err == nil {
			adResp, err2 := idClient.ListAvailabilityDomains(ctx, identity.ListAvailabilityDomainsRequest{
				CompartmentId: common.String(profile.TenancyOCID),
			})
			if err2 == nil {
				for _, ad := range adResp.Items {
					adList = append(adList, *ad.Name)
				}
			}
		}
	}
	if len(adList) == 0 {
		msg := "未找到可用区 (AD)，请检查网络或区域配置"
		storage.DB.Model(&task).Updates(map[string]interface{}{
			"status":       "failed",
			"last_message": msg,
		})
		return
	}

	currentADIndex := 0
	backoffFactor := 1

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Engine] Task %s worker stopped via context", taskID)
			return
		default:
		}

		// Re-check task status in DB
		var currentTask storage.LaunchTask
		if err := storage.DB.First(&currentTask, "id = ?", taskID).Error; err != nil || currentTask.Status != "running" {
			log.Printf("[Engine] Task %s is no longer in running status (%s), exiting loop", taskID, currentTask.Status)
			return
		}

		// Single-Account Mutex Lock renewal
		_, _, _ = cache.AcquireAccountLock(ctx, profile.ID, 45*time.Second)

		// 1. Idempotency Check: check if instance with same name already exists in target region!
		computeClient, err := oci.GetComputeClient(&profile, task.Region)
		if err == nil {
			instList, err2 := computeClient.ListInstances(ctx, core.ListInstancesRequest{
				CompartmentId: common.String(profile.TenancyOCID),
				DisplayName:   common.String(task.InstanceName),
			})
			if err2 == nil {
				for _, inst := range instList.Items {
					if inst.LifecycleState != core.InstanceLifecycleStateTerminated && inst.LifecycleState != core.InstanceLifecycleStateTerminating {
						// Instance already created! Mark success immediately!
						instOCID := common.StringToEmptyString(inst.Id)
						log.Printf("[Engine] Found existing instance %s (%s)! Marking task success.", task.InstanceName, instOCID)

						// Fetch public IP
						pubIP := ""
						ipv6 := ""
						details, err3 := oci.ListInstancesWithDetails(ctx, &profile, task.Region)
						if err3 == nil {
							for _, d := range details {
								if d.OCID == instOCID {
									pubIP = d.PublicIP
									ipv6 = d.IPv6
									break
								}
							}
						}

						storage.DB.Model(&currentTask).Updates(map[string]interface{}{
							"status":                "success",
							"success_instance_ocid": instOCID,
							"success_public_ip":     pubIP,
							"success_ipv6":          ipv6,
							"last_message":          "开机成功（已检测到云端存活同名实例，防重复生效）",
						})

						notify.NotifyTaskSuccess(&currentTask, &profile, pubIP, ipv6, currentTask.RootPasswordEnc)
						emitLog(ctx, taskID.String(), currentTask.CurrentRetries+1, task.Region, "", "SUCCESS", "开机成功！已捕获云端实例，自动停止抢机", 0)
						return
					}
				}
			}
		}

		// Select current AD
		targetAD := adList[currentADIndex%len(adList)]
		currentADIndex++

		currentRetries := currentTask.CurrentRetries + 1
		startTime := time.Now()

		// 2. Execute launch instance call
		instanceOCID, launchErr := oci.LaunchInstance(ctx, &profile, &currentTask, targetAD)
		durationMs := time.Since(startTime).Milliseconds()

		now := time.Now()
		storage.DB.Model(&currentTask).Updates(map[string]interface{}{
			"current_retries": currentRetries,
			"last_attempt_at": &now,
		})

		if launchErr == nil && instanceOCID != "" {
			// SUCCESS!
			log.Printf("[Engine] Task %s successfully launched instance %s!", taskID, instanceOCID)

			// Wait a few seconds for VNIC allocation
			time.Sleep(10 * time.Second)
			pubIP := ""
			ipv6 := ""
			details, _ := oci.ListInstancesWithDetails(ctx, &profile, task.Region)
			for _, d := range details {
				if d.OCID == instanceOCID {
					pubIP = d.PublicIP
					ipv6 = d.IPv6
					break
				}
			}

			storage.DB.Model(&currentTask).Updates(map[string]interface{}{
				"status":                "success",
				"success_instance_ocid": instanceOCID,
				"success_public_ip":     pubIP,
				"success_ipv6":          ipv6,
				"last_message":          "开机成功！已分配公网 IP",
			})

			// Record attempt
			storage.DB.Create(&storage.TaskAttempt{
				TaskID:          taskID,
				AttemptNum:      currentRetries,
				Region:          task.Region,
				AD:              targetAD,
				Status:          "success",
				ResponseMessage: "创建实例成功 (Instance Launched)",
				DurationMs:      durationMs,
				CreatedAt:       time.Now(),
			})

			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "SUCCESS", fmt.Sprintf("🎉 抢机成功！实例 OCID: %s, 公网 IP: %s", instanceOCID, pubIP), durationMs)
			notify.NotifyTaskSuccess(&currentTask, &profile, pubIP, ipv6, currentTask.RootPasswordEnc)
			return
		}

		// Handle error
		category, reason := ClassifyError(launchErr)

		switch category {
		case CategoryFatalError:
			// Stop immediately, never retry bad configs
			log.Printf("[Engine] Task %s encountered fatal error: %v", taskID, launchErr)
			storage.DB.Model(&currentTask).Updates(map[string]interface{}{
				"status":       "failed",
				"last_message": fmt.Sprintf("致命错误熔断: %s", reason),
			})

			storage.DB.Create(&storage.TaskAttempt{
				TaskID:          taskID,
				AttemptNum:      currentRetries,
				Region:          task.Region,
				AD:              targetAD,
				Status:          "fatal_error",
				ResponseMessage: reason,
				DurationMs:      durationMs,
				CreatedAt:       time.Now(),
			})

			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "FATAL", fmt.Sprintf("❌ 致命配置错误，任务立即熔断停止: %s", reason), durationMs)
			notify.NotifyTaskFatalError(&currentTask, &profile, reason)
			return

		case CategoryRateLimited:
			// Exponential backoff
			backoffFactor *= 2
			if backoffFactor > 16 {
				backoffFactor = 16
			}
			waitSecs := currentTask.RetryIntervalSecs * backoffFactor
			storage.DB.Create(&storage.TaskAttempt{
				TaskID:          taskID,
				AttemptNum:      currentRetries,
				Region:          task.Region,
				AD:              targetAD,
				Status:          "rate_limited",
				ResponseMessage: reason,
				DurationMs:      durationMs,
				CreatedAt:       time.Now(),
			})

			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "RATE_LIMIT", fmt.Sprintf("⚠️ 429 限流退避中，等待 %d 秒后重试", waitSecs), durationMs)
			time.Sleep(time.Duration(waitSecs) * time.Second)

		case CategoryCapacityFull:
			// Out of host capacity -> retry with jitter
			backoffFactor = 1
			jitter := rand.Intn(10) // 0-10s jitter
			waitSecs := currentTask.RetryIntervalSecs + jitter

			storage.DB.Create(&storage.TaskAttempt{
				TaskID:          taskID,
				AttemptNum:      currentRetries,
				Region:          task.Region,
				AD:              targetAD,
				Status:          "capacity_full",
				ResponseMessage: reason,
				DurationMs:      durationMs,
				CreatedAt:       time.Now(),
			})

			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "RETRY", fmt.Sprintf("容量不足 (Out of host capacity)，%d 秒后在下一可用区重试", waitSecs), durationMs)

			// Sleep for wait interval
			time.Sleep(time.Duration(waitSecs) * time.Second)
		}

		// Check max retries
		if currentTask.MaxRetries > 0 && currentRetries >= currentTask.MaxRetries {
			storage.DB.Model(&currentTask).Updates(map[string]interface{}{
				"status":       "stopped",
				"last_message": "已达到最大尝试次数限制，任务自动停止",
			})
			emitLog(ctx, taskID.String(), currentRetries, task.Region, targetAD, "STOPPED", "已达到设定的最大重试次数，任务停止", durationMs)
			return
		}
	}
}

func emitLog(ctx context.Context, taskID string, attemptNum int, region, ad, status, message string, durationMs int64) {
	logMsg := LiveLogMessage{
		TaskID:     taskID,
		AttemptNum: attemptNum,
		Timestamp:  time.Now().Format("15:04:05"),
		Region:     region,
		AD:         ad,
		Status:     status,
		Message:    message,
		DurationMs: durationMs,
	}

	payload, err := json.Marshal(logMsg)
	if err == nil {
		_ = cache.PublishTaskLog(ctx, taskID, string(payload))
	}
}
