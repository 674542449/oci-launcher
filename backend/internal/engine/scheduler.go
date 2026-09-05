package engine

import (
	"context"
	"log"
	"sync"

	"oci-panel/internal/storage"

	"github.com/google/uuid"
)

var (
	activeTasks sync.Map // taskID (string) -> context.CancelFunc
)

// StartTask initiates the background worker for a task
func StartTask(taskID uuid.UUID) error {
	// Stop existing worker if already running
	StopTask(taskID)

	// Update DB status
	err := storage.DB.Model(&storage.LaunchTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":       "running",
			"last_message": "后台抢机调度中...",
		}).Error
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	activeTasks.Store(taskID.String(), cancel)

	go func() {
		defer func() {
			activeTasks.Delete(taskID.String())
			if r := recover(); r != nil {
				log.Printf("[Scheduler] Worker panic recovered for task %s: %v", taskID, r)
			}
		}()
		RunTaskWorker(ctx, taskID)
	}()

	return nil
}

// StopTask cancels the background worker for a task
func StopTask(taskID uuid.UUID) {
	if cancelVal, ok := activeTasks.Load(taskID.String()); ok {
		if cancel, ok2 := cancelVal.(context.CancelFunc); ok2 {
			cancel()
		}
		activeTasks.Delete(taskID.String())
	}

	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "用户手动停止抢机任务",
		}).Error
}

// ResumeAllRunningTasks recovers running tasks upon server restart
func ResumeAllRunningTasks() {
	var tasks []storage.LaunchTask
	if err := storage.DB.Where("status = ?", "running").Find(&tasks).Error; err == nil {
		log.Printf("[Scheduler] Resuming %d active tasks from database...", len(tasks))
		for _, t := range tasks {
			_ = StartTask(t.ID)
		}
	}
}

// PanicLockdown stops all tasks and freezes operations
func PanicLockdown() {
	log.Println("🚨 [Scheduler] PANIC LOCKDOWN TRIGGERED! Stopping all active workers...")
	activeTasks.Range(func(key, value interface{}) bool {
		if cancel, ok := value.(context.CancelFunc); ok {
			cancel()
		}
		return true
	})

	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("status = ?", "running").
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "【全站紧急锁定】所有任务已被安全停机",
		}).Error
}
