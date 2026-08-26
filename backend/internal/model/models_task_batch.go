package model

import "time"

type TaskBatchStatus string
type TaskBatchItemStatus string

const (
	TaskBatchStatusQueued              TaskBatchStatus = "queued"
	TaskBatchStatusRunning             TaskBatchStatus = "running"
	TaskBatchStatusPaused              TaskBatchStatus = "paused"
	TaskBatchStatusSucceeded           TaskBatchStatus = "succeeded"
	TaskBatchStatusCompletedWithErrors TaskBatchStatus = "completed_with_errors"
	TaskBatchStatusCancelled           TaskBatchStatus = "cancelled"

	TaskBatchItemStatusWaiting    TaskBatchItemStatus = "waiting"
	TaskBatchItemStatusSubmitting TaskBatchItemStatus = "submitting"
	TaskBatchItemStatusQueued     TaskBatchItemStatus = "queued"
	TaskBatchItemStatusRunning    TaskBatchItemStatus = "running"
	TaskBatchItemStatusSucceeded  TaskBatchItemStatus = "succeeded"
	TaskBatchItemStatusFailed     TaskBatchItemStatus = "failed"
	TaskBatchItemStatusCancelled  TaskBatchItemStatus = "cancelled"
)

// TaskBatch is a durable user command. Waiting items remain outside the normal
// task queue until capacity is available, so a 1000-item batch does not bypass
// the per-account active task limit.
type TaskBatch struct {
	ID             string          `json:"id" gorm:"primaryKey;size:36"`
	UserID         string          `json:"userId" gorm:"size:36;index;uniqueIndex:idx_task_batches_user_idempotency,priority:1"`
	ProjectID      string          `json:"projectId,omitempty" gorm:"size:80;index"`
	Mode           string          `json:"mode" gorm:"size:24;index"`
	Status         TaskBatchStatus `json:"status" gorm:"size:32;index"`
	RequestedCount int             `json:"requestedCount"`
	WaitingCount   int             `json:"waitingCount"`
	QueuedCount    int             `json:"queuedCount"`
	RunningCount   int             `json:"runningCount"`
	SucceededCount int             `json:"succeededCount"`
	FailedCount    int             `json:"failedCount"`
	CancelledCount int             `json:"cancelledCount"`
	IdempotencyKey string          `json:"-" gorm:"size:160;uniqueIndex:idx_task_batches_user_idempotency,priority:2"`
	RequestJSON    string          `json:"-" gorm:"type:text"`
	LastError      string          `json:"lastError,omitempty" gorm:"size:1000"`
	CreatedAt      time.Time       `json:"createdAt" gorm:"index"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type TaskBatchItem struct {
	ID             string              `json:"id" gorm:"primaryKey;size:36"`
	BatchID        string              `json:"batchId" gorm:"size:36;index;uniqueIndex:idx_task_batch_items_batch_index,priority:1"`
	Index          int                 `json:"index" gorm:"column:item_index;uniqueIndex:idx_task_batch_items_batch_index,priority:2"`
	TaskID         string              `json:"taskId,omitempty" gorm:"size:36;index"`
	Status         TaskBatchItemStatus `json:"status" gorm:"size:24;index"`
	RetryCount     int                 `json:"retryCount"`
	RetryRequested bool                `json:"retryRequested,omitempty" gorm:"index"`
	Error          string              `json:"error,omitempty" gorm:"size:1000"`
	ClaimOwner     string              `json:"-" gorm:"size:120;index"`
	ClaimExpiresAt *time.Time          `json:"-" gorm:"index"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}
