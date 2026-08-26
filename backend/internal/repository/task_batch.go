package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrTaskBatchIdempotency = errors.New("task batch idempotency key already exists")

func (r *Repository) CreateTaskBatch(batch *model.TaskBatch, items []model.TaskBatchItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		created := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(batch)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return ErrTaskBatchIdempotency
		}
		return tx.CreateInBatches(items, 100).Error
	})
}

func (r *Repository) TaskBatchByIdempotencyKey(userID string, key string) (*model.TaskBatch, error) {
	var batch model.TaskBatch
	if err := r.db.First(&batch, "user_id = ? AND idempotency_key = ?", userID, key).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *Repository) TaskBatchForUser(userID string, id string) (*model.TaskBatch, error) {
	var batch model.TaskBatch
	if err := r.db.First(&batch, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *Repository) TaskBatch(id string) (*model.TaskBatch, error) {
	var batch model.TaskBatch
	if err := r.db.First(&batch, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *Repository) TaskBatchesForUser(userID string, limit int) ([]model.TaskBatch, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var batches []model.TaskBatch
	err := r.db.Where("user_id = ?", userID).Order("created_at desc").Limit(limit).Find(&batches).Error
	return batches, err
}

func (r *Repository) TaskBatchItems(batchID string) ([]model.TaskBatchItem, error) {
	var items []model.TaskBatchItem
	err := r.db.Where("batch_id = ?", batchID).Order("item_index asc").Find(&items).Error
	return items, err
}

func (r *Repository) TasksByIDsForUser(userID string, ids []string) ([]model.Task, error) {
	if len(ids) == 0 {
		return []model.Task{}, nil
	}
	var tasks []model.Task
	err := r.db.Where("user_id = ? AND id IN ?", userID, ids).Find(&tasks).Error
	return tasks, err
}

func (r *Repository) TaskByBatchItemID(batchItemID string) (*model.Task, error) {
	var task model.Task
	if err := r.db.First(&task, "batch_item_id = ?", batchItemID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ClaimNextTaskBatchItem uses a conditional update for both SQLite and
// PostgreSQL. Updating the parent timestamp rotates active batches so one large
// batch cannot permanently starve newer users.
func (r *Repository) ClaimNextTaskBatchItem(owner string, leaseDuration time.Duration) (*model.TaskBatch, *model.TaskBatchItem, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(leaseDuration)
	var claimed model.TaskBatchItem
	var batch model.TaskBatch
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var candidate model.TaskBatchItem
		query := tx.Model(&model.TaskBatchItem{}).
			Joins("JOIN task_batches ON task_batches.id = task_batch_items.batch_id").
			Where("task_batches.status IN ?", []model.TaskBatchStatus{model.TaskBatchStatusQueued, model.TaskBatchStatusRunning}).
			Where("task_batch_items.status = ? OR (task_batch_items.status = ? AND task_batch_items.claim_expires_at <= ?)", model.TaskBatchItemStatusWaiting, model.TaskBatchItemStatusSubmitting, now).
			Order("task_batches.updated_at asc").Order("task_batch_items.item_index asc").Limit(1)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		result := query.Find(&candidate)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		updated := tx.Model(&model.TaskBatchItem{}).
			Where("id = ? AND (status = ? OR (status = ? AND claim_expires_at <= ?))", candidate.ID, model.TaskBatchItemStatusWaiting, model.TaskBatchItemStatusSubmitting, now).
			Updates(map[string]any{"status": model.TaskBatchItemStatusSubmitting, "claim_owner": owner, "claim_expires_at": leaseExpiresAt, "updated_at": now})
		if updated.Error != nil || updated.RowsAffected == 0 {
			return updated.Error
		}
		if err := tx.Model(&model.TaskBatch{}).Where("id = ?", candidate.BatchID).Updates(map[string]any{
			"status":     model.TaskBatchStatusRunning,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.First(&claimed, "id = ?", candidate.ID).Error; err != nil {
			return err
		}
		return tx.First(&batch, "id = ?", candidate.BatchID).Error
	})
	if err != nil || claimed.ID == "" {
		return nil, nil, err
	}
	return &batch, &claimed, nil
}

func (r *Repository) LinkTaskBatchItem(itemID string, taskID string, status model.TaskBatchItemStatus) error {
	return r.db.Model(&model.TaskBatchItem{}).Where("id = ?", itemID).Updates(map[string]any{
		"task_id": taskID, "status": status, "retry_requested": false, "error": "",
		"claim_owner": "", "claim_expires_at": nil, "updated_at": time.Now(),
	}).Error
}

func (r *Repository) ReleaseTaskBatchItem(itemID string, status model.TaskBatchItemStatus, errorText string) error {
	return r.db.Model(&model.TaskBatchItem{}).Where("id = ?", itemID).Updates(map[string]any{
		"status": status, "error": errorText, "claim_owner": "", "claim_expires_at": nil, "updated_at": time.Now(),
	}).Error
}

func (r *Repository) UpdateTaskBatchItemFromTask(itemID string, status model.TaskBatchItemStatus, errorText string) error {
	return r.db.Model(&model.TaskBatchItem{}).Where("id = ? AND retry_requested = ?", itemID, false).Updates(map[string]any{
		"status": status, "error": errorText, "claim_owner": "", "claim_expires_at": nil, "updated_at": time.Now(),
	}).Error
}

func (r *Repository) UpdateTaskBatchSnapshot(batchID string, values map[string]any) error {
	values["updated_at"] = time.Now()
	return r.db.Model(&model.TaskBatch{}).Where("id = ?", batchID).Updates(values).Error
}

func (r *Repository) ActiveTaskBatches() ([]model.TaskBatch, error) {
	var batches []model.TaskBatch
	err := r.db.Where("status IN ? OR (status IN ? AND (queued_count + running_count) > 0)",
		[]model.TaskBatchStatus{model.TaskBatchStatusQueued, model.TaskBatchStatusRunning},
		[]model.TaskBatchStatus{model.TaskBatchStatusPaused, model.TaskBatchStatusCancelled},
	).Find(&batches).Error
	return batches, err
}

func (r *Repository) PauseTaskBatchForUser(userID string, id string, lastError string) error {
	updated := r.db.Model(&model.TaskBatch{}).Where("id = ? AND user_id = ? AND status IN ?", id, userID, []model.TaskBatchStatus{model.TaskBatchStatusQueued, model.TaskBatchStatusRunning}).Updates(map[string]any{
		"status": model.TaskBatchStatusPaused, "last_error": lastError, "updated_at": time.Now(),
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) ResumeTaskBatchForUser(userID string, id string) error {
	updated := r.db.Model(&model.TaskBatch{}).Where("id = ? AND user_id = ? AND status = ?", id, userID, model.TaskBatchStatusPaused).Updates(map[string]any{
		"status": model.TaskBatchStatusQueued, "last_error": "", "completed_at": nil, "updated_at": time.Now(),
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) CancelWaitingTaskBatchItemsForUser(userID string, batchID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var batch model.TaskBatch
		if err := tx.First(&batch, "id = ? AND user_id = ?", batchID, userID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TaskBatchItem{}).Where("batch_id = ? AND status IN ?", batchID, []model.TaskBatchItemStatus{model.TaskBatchItemStatusWaiting, model.TaskBatchItemStatusSubmitting}).Updates(map[string]any{
			"status": model.TaskBatchItemStatusCancelled, "retry_requested": false, "claim_owner": "", "claim_expires_at": nil, "updated_at": time.Now(),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.TaskBatch{}).Where("id = ?", batchID).Updates(map[string]any{"status": model.TaskBatchStatusCancelled, "updated_at": time.Now()}).Error
	})
}

func (r *Repository) RetryFailedTaskBatchItemsForUser(userID string, batchID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var batch model.TaskBatch
		if err := tx.First(&batch, "id = ? AND user_id = ?", batchID, userID).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.TaskBatchItem{}).Where("batch_id = ? AND status = ?", batchID, model.TaskBatchItemStatusFailed).Updates(map[string]any{
			"status": model.TaskBatchItemStatusWaiting, "retry_requested": true, "retry_count": gorm.Expr("retry_count + ?", 1), "error": "", "updated_at": time.Now(),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&model.TaskBatch{}).Where("id = ?", batchID).Updates(map[string]any{"status": model.TaskBatchStatusQueued, "last_error": "", "completed_at": nil, "updated_at": time.Now()}).Error
	})
}
