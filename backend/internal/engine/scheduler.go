package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"oci-panel/internal/storage"

	"github.com/google/uuid"
)

type workerHandle struct {
	cancel context.CancelFunc
}

var activeTasks sync.Map // taskID (string) -> *workerHandle

// cancelWorker stops the goroutine of a task without touching the database.
func cancelWorker(taskID uuid.UUID) {
	key := taskID.String()
	if v, ok := activeTasks.Load(key); ok {
		if h, ok2 := v.(*workerHandle); ok2 {
			h.cancel()
			activeTasks.CompareAndDelete(key, v)
		}
	}
}

// IsTaskActive reports whether a worker goroutine is registered for the task.
func IsTaskActive(taskID uuid.UUID) bool {
	_, ok := activeTasks.Load(taskID.String())
	return ok
}

// StartTask (re)starts the background worker for a task.
func StartTask(taskID uuid.UUID) error {
	cancelWorker(taskID)

	err := storage.DB.Model(&storage.LaunchTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":       "running",
			"last_message": "已加入排队，等待下一次尝试…",
		}).Error
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	handle := &workerHandle{cancel: cancel}
	key := taskID.String()
	activeTasks.Store(key, handle)

	go func() {
		defer func() {
			// Only remove our own registration: a restarted task may already own a newer handle.
			activeTasks.CompareAndDelete(key, handle)
			if r := recover(); r != nil {
				log.Printf("[Scheduler] Worker panic recovered for task %s: %v", taskID, r)
				_ = storage.DB.Model(&storage.LaunchTask{}).
					Where("id = ? AND status = ?", taskID, "running").
					Updates(map[string]interface{}{
						"status":       "failed",
						"last_message": fmt.Sprintf("内部错误，任务已停止: %v", r),
					}).Error
			}
		}()
		RunTaskWorker(ctx, taskID)
	}()

	return nil
}

// StopTask cancels the worker and marks a running task as stopped (terminal states are kept).
func StopTask(taskID uuid.UUID) {
	cancelWorker(taskID)

	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("id = ? AND status = ?", taskID, "running").
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "用户手动停止排队",
		}).Error

	// A record stuck in the synchronous "creating" state can be cleared by the user as well;
	// if the create flow is in fact still running, its own final write wins afterwards.
	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("id = ? AND status = ?", taskID, "creating").
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "用户手动清除，请到「实例」页确认结果",
		}).Error
}

// ResumeAllRunningTasks recovers running tasks after a restart
func ResumeAllRunningTasks() {
	// A synchronous first attempt that was interrupted by the restart cannot be resumed as such
	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("status = ?", "creating").
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "服务重启时创建被中断，可点击「重试」重新排队",
		}).Error

	var tasks []storage.LaunchTask
	if err := storage.DB.Where("status = ?", "running").Find(&tasks).Error; err == nil {
		log.Printf("[Scheduler] Resuming %d active tasks from database...", len(tasks))
		for _, t := range tasks {
			_ = StartTask(t.ID)
		}
	}
}

// PanicLockdown stops all workers and marks their tasks stopped
func PanicLockdown() {
	log.Println("[Scheduler] PANIC LOCKDOWN TRIGGERED! Stopping all active workers...")
	activeTasks.Range(func(key, value interface{}) bool {
		if h, ok := value.(*workerHandle); ok {
			h.cancel()
		}
		activeTasks.Delete(key)
		return true
	})

	_ = storage.DB.Model(&storage.LaunchTask{}).
		Where("status = ?", "running").
		Updates(map[string]interface{}{
			"status":       "stopped",
			"last_message": "【全站紧急锁定】所有任务已被安全停机",
		}).Error
}
