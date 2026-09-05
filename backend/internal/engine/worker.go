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
	retryWaitMinSecs      = 60
	retryWaitMaxSecs      = 180
	maxBackoffFactor      = 8
	instanceIPTimeout     = 5 * time.Minute // how long to wait for a requested public IPv4
	instanceReadyInterval = 10 * time.Second
	// AddressWatchTimeout bounds one background FinalizeSuccess run.
	AddressWatchTimeout = 6 * time.Minute
)

// AttemptResult is the outcome of one LaunchInstance attempt.
type AttemptResult struct {
	Success        bool
	AlreadyExisted bool
	InstanceOCID   string
	Category       ErrorCategory
	Reason         string
	DurationMs     int64
	AD             string
}

// Retryable reports whether the failure is worth another attempt later.
func (r AttemptResult) Retryable() bool {
	return !r.Success && (r.Category == CategoryCapacityFull || r.Category == CategoryRateLimited || r.Category == CategoryTransient)
}

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

// ResolveADList returns the task's AD preference, or every AD of the region when unset.
func ResolveADList(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask) ([]string, error) {
	if adList := ParseADList(task.ADList); len(adList) > 0 {
		return adList, nil
	}
	names, err := oci.ListAvailabilityDomainNames(ctx, profile, task.Region)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("region %s has no availability domains", task.Region)
	}
	return names, nil
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

// RandomRetryWait returns the pause between queued attempts: 60-180 s, uniformly random.
func RandomRetryWait() time.Duration {
	return time.Duration(retryWaitMinSecs+rand.Intn(retryWaitMaxSecs-retryWaitMinSecs+1)) * time.Second
}

var (
	nameAdjectives = []string{"amber", "brisk", "calm", "coral", "crisp", "dusk", "ember", "fern", "frost", "gale", "glen", "hazel", "ivory", "jade", "lunar", "maple", "misty", "nova", "ocean", "onyx", "opal", "pearl", "pine", "quiet", "river", "sage", "slate", "solar", "storm", "swift", "terra", "tidal", "vivid", "willow", "zephyr"}
	nameNouns      = []string{"atlas", "beacon", "cedar", "comet", "delta", "falcon", "harbor", "heron", "iris", "kestrel", "lantern", "lotus", "meadow", "nimbus", "orbit", "osprey", "pixel", "quartz", "ridge", "sparrow", "summit", "tundra", "vector", "vertex", "voyager", "willet", "zenith"}
)

// RandomInstanceName produces a project-style name such as "amber-falcon-42".
func RandomInstanceName() string {
	return fmt.Sprintf("%s-%s-%02d", nameAdjectives[rand.Intn(len(nameAdjectives))], nameNouns[rand.Intn(len(nameNouns))], rand.Intn(100))
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

// findExistingInstance looks for a live instance with the task's display name (idempotency).
func findExistingInstance(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask) (string, bool) {
	computeClient, err := oci.GetComputeClient(profile, task.Region)
	if err != nil {
		return "", false
	}
	resp, err := computeClient.ListInstances(ctx, core.ListInstancesRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		DisplayName:   common.String(task.InstanceName),
	})
	if err != nil {
		return "", false
	}
	for _, inst := range resp.Items {
		if inst.LifecycleState == core.InstanceLifecycleStateTerminated || inst.LifecycleState == core.InstanceLifecycleStateTerminating {
			continue
		}
		return oci.StrVal(inst.Id), true
	}
	return "", false
}

// AttemptLaunch performs one LaunchInstance attempt (after the same-name idempotency check),
// persists the attempt row and publishes a live-log line. It does not change the task status.
func AttemptLaunch(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask, targetAD string, attemptNum int, rootPassword string) AttemptResult {
	res := AttemptResult{AD: targetAD}

	if ocid, found := findExistingInstance(ctx, profile, task); found {
		res.Success, res.AlreadyExisted, res.InstanceOCID = true, true, ocid
		return res
	}
	if ctx.Err() != nil {
		res.Category = CategoryCancelled
		return res
	}

	launchTask := *task
	launchTask.RootPasswordEnc = rootPassword
	start := time.Now()
	instanceOCID, launchErr := oci.LaunchInstance(ctx, profile, &launchTask, targetAD)
	res.DurationMs = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		res.Category = CategoryCancelled
		return res
	}

	now := time.Now()
	storage.DB.Model(&storage.LaunchTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"current_retries": attemptNum,
		"last_attempt_at": &now,
	})

	if launchErr == nil && instanceOCID != "" {
		res.Success, res.InstanceOCID = true, instanceOCID
		recordAttempt(task.ID, attemptNum, task.Region, targetAD, "success", "创建实例成功 (Instance Launched)", res.DurationMs)
		emitLog(task.ID.String(), attemptNum, task.Region, targetAD, "SUCCESS", "实例创建成功（已返回实例 OCID），正在获取公网 IP…", res.DurationMs)
		return res
	}

	res.Category, res.Reason = ClassifyError(launchErr)
	switch res.Category {
	case CategoryCancelled:
		return res
	case CategoryFatalError:
		recordAttempt(task.ID, attemptNum, task.Region, targetAD, "fatal_error", res.Reason, res.DurationMs)
		emitLog(task.ID.String(), attemptNum, task.Region, targetAD, "FATAL", "配置或凭据错误: "+res.Reason, res.DurationMs)
	case CategoryRateLimited:
		recordAttempt(task.ID, attemptNum, task.Region, targetAD, "rate_limited", res.Reason, res.DurationMs)
		emitLog(task.ID.String(), attemptNum, task.Region, targetAD, "RATE_LIMIT", res.Reason, res.DurationMs)
	case CategoryTransient:
		recordAttempt(task.ID, attemptNum, task.Region, targetAD, "transient_error", res.Reason, res.DurationMs)
		emitLog(task.ID.String(), attemptNum, task.Region, targetAD, "RETRY", res.Reason, res.DurationMs)
	default:
		recordAttempt(task.ID, attemptNum, task.Region, targetAD, "capacity_full", res.Reason, res.DurationMs)
		emitLog(task.ID.String(), attemptNum, task.Region, targetAD, "RETRY", fmt.Sprintf("%s（可用区 %s）", res.Reason, shortAD(targetAD)), res.DurationMs)
	}
	return res
}

func shortAD(ad string) string {
	if i := strings.Index(ad, ":"); i >= 0 {
		return ad[i+1:]
	}
	return ad
}

// SuccessFields is the task update written the moment LaunchInstance returns an instance OCID:
// that is the success criterion, nothing after it changes the status.
func SuccessFields(instanceOCID string, existed bool) map[string]interface{} {
	msg := "实例创建成功，正在获取公网 IP"
	if existed {
		msg = "云端已存在同名实例，视为创建成功"
	}
	return map[string]interface{}{
		"status":                "success",
		"success_instance_ocid": instanceOCID,
		"last_message":          msg,
	}
}

// waitForPublicIP reads the primary VNIC's addresses; when a public IPv4 was requested it keeps
// polling for it up to instanceIPTimeout (the VNIC appears a few seconds after acceptance).
func waitForPublicIP(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, wantPublicIP bool) (pubIP, ipv6 string) {
	deadline := time.Now().Add(instanceIPTimeout)
	for {
		ip, v6, err := oci.GetPrimaryVnicAddresses(ctx, profile, region, instanceOCID)
		if err == nil {
			pubIP, ipv6 = ip, v6
			if pubIP != "" || !wantPublicIP {
				return
			}
		}
		if time.Now().After(deadline) || !sleepCtx(ctx, instanceReadyInterval) {
			return
		}
	}
}

// FinalizeSuccess runs after the task has already been marked success: it looks up the new
// instance's addresses, stores them on the task and sends the notification. It never changes
// the task status. Safe to run in its own goroutine with a background context.
func FinalizeSuccess(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask, res AttemptResult, attemptNum int, rootPassword string) {
	pubIP, ipv6 := waitForPublicIP(ctx, profile, task.Region, res.InstanceOCID, task.AssignPublicIP)
	if ctx.Err() != nil {
		return
	}

	msg := "实例创建成功"
	if res.AlreadyExisted {
		msg = "云端已存在同名实例，视为创建成功"
	}
	if pubIP != "" {
		msg += "，公网 IP " + pubIP
	} else if task.AssignPublicIP {
		msg += "，公网 IP 尚未分配，可稍后在「实例」页刷新"
	}

	storage.DB.Model(&storage.LaunchTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"success_public_ip": pubIP,
		"success_ipv6":      ipv6,
		"last_message":      msg,
	})
	emitLog(task.ID.String(), attemptNum, task.Region, res.AD, "SUCCESS", fmt.Sprintf("%s。实例 OCID: %s", msg, res.InstanceOCID), 0)
	notify.NotifyTaskSuccess(task, profile, pubIP, ipv6, rootPassword)
}

// RunTaskWorker is the queued retry loop: it keeps attempting LaunchInstance, rotating through
// the availability domains, until it succeeds, hits a fatal error or is stopped.
func RunTaskWorker(ctx context.Context, taskID uuid.UUID) {
	log.Printf("[Engine] Starting worker for task: %s", taskID)

	var task storage.LaunchTask
	if err := storage.DB.First(&task, "id = ?", taskID).Error; err != nil {
		log.Printf("[Engine] Task %s not found in database", taskID)
		return
	}

	var profile storage.OCIProfile
	if err := storage.DB.First(&profile, "id = ?", task.ProfileID).Error; err != nil {
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

	adList, err := ResolveADList(ctx, &profile, &task)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		setTaskStatus(taskID, "failed", "无法获取可用区列表: "+err.Error())
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

		// One OCI account operated at a time: take (or refresh) the global lock
		if ok, lockedBy, err := cache.AcquireAccountLock(ctx, profile.ID, time.Duration(retryWaitMaxSecs*2)*time.Second); err != nil {
			log.Printf("[Engine] Task %s: account lock error: %v", taskID, err)
		} else if !ok {
			wait := RandomRetryWait()
			emitLog(taskID.String(), currentTask.CurrentRetries, task.Region, "", "WAIT",
				fmt.Sprintf("等待账号并发锁：账号 ID %s 正在操作中，%d 秒后再试", lockedBy, int(wait.Seconds())), 0)
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}

		targetAD := adList[currentADIndex%len(adList)]
		currentADIndex++
		attemptNum := currentTask.CurrentRetries + 1

		res := AttemptLaunch(ctx, &profile, &currentTask, targetAD, attemptNum, rootPassword)
		if res.Category == CategoryCancelled && !res.Success {
			return
		}

		if res.Success {
			// The OCID is the success criterion; addresses and the notification follow.
			storage.DB.Model(&storage.LaunchTask{}).Where("id = ?", taskID).Updates(SuccessFields(res.InstanceOCID, res.AlreadyExisted))
			FinalizeSuccess(ctx, &profile, &currentTask, res, attemptNum, rootPassword)
			return
		}

		var wait time.Duration
		switch res.Category {
		case CategoryFatalError:
			setTaskStatus(taskID, "failed", "致命错误熔断: "+res.Reason)
			notify.NotifyTaskFatalError(&currentTask, &profile, res.Reason)
			return
		case CategoryRateLimited:
			backoffFactor *= 2
			if backoffFactor > maxBackoffFactor {
				backoffFactor = maxBackoffFactor
			}
			wait = RandomRetryWait() * time.Duration(backoffFactor)
		default:
			backoffFactor = 1
			wait = RandomRetryWait()
		}

		// Attempt budget is checked before sleeping so the last attempt does not waste an interval
		if currentTask.MaxRetries > 0 && attemptNum >= currentTask.MaxRetries {
			setTaskStatus(taskID, "stopped", "已达到最大尝试次数限制，任务自动停止")
			emitLog(taskID.String(), attemptNum, task.Region, targetAD, "STOPPED", "已达到设定的最大重试次数，任务停止", res.DurationMs)
			return
		}

		storage.DB.Model(&storage.LaunchTask{}).Where("id = ?", taskID).Update("last_message",
			fmt.Sprintf("%s，%d 秒后重试（第 %d 次）", res.Reason, int(wait.Seconds()), attemptNum))
		emitLog(taskID.String(), attemptNum, task.Region, targetAD, "WAIT", fmt.Sprintf("%d 秒后进行第 %d 次尝试", int(wait.Seconds()), attemptNum+1), 0)
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

func emitLog(taskID string, attemptNum int, region, ad, status, message string, durationMs int64) {
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
	// Publishing must work even while a worker context is being cancelled
	bg, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = cache.PublishTaskLog(bg, taskID, string(payload))
}
