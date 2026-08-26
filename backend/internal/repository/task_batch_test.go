package repository

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskBatchPersistsOneThousandItemsAndClaimsDurably(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-batch-1000?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TaskBatch{}, &model.TaskBatchItem{}, &model.Task{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	now := time.Now()
	batch := model.TaskBatch{ID: "batch-1", UserID: "user-1", Mode: "image", Status: model.TaskBatchStatusQueued, RequestedCount: 1000, WaitingCount: 1000, IdempotencyKey: "request-1", RequestJSON: `{}`, CreatedAt: now, UpdatedAt: now}
	items := make([]model.TaskBatchItem, batch.RequestedCount)
	for index := range items {
		items[index] = model.TaskBatchItem{ID: fmt.Sprintf("item-%04d", index), BatchID: batch.ID, Index: index, Status: model.TaskBatchItemStatusWaiting, CreatedAt: now, UpdatedAt: now}
	}
	if err := repo.CreateTaskBatch(&batch, items); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.TaskBatchItems(batch.ID)
	if err != nil || len(stored) != 1000 {
		t.Fatalf("stored items = %d, error = %v", len(stored), err)
	}
	claimedBatch, claimed, err := repo.ClaimNextTaskBatchItem("worker-1", time.Minute)
	if err != nil || claimed == nil || claimedBatch.ID != batch.ID || claimed.Index != 0 || claimed.Status != model.TaskBatchItemStatusSubmitting {
		t.Fatalf("claim = %#v batch = %#v error = %v", claimed, claimedBatch, err)
	}
	if err := repo.LinkTaskBatchItem(claimed.ID, "task-1", model.TaskBatchItemStatusQueued); err != nil {
		t.Fatal(err)
	}
	linked, err := repo.TaskBatchItems(batch.ID)
	if err != nil || linked[0].TaskID != "task-1" || linked[0].Status != model.TaskBatchItemStatusQueued {
		t.Fatalf("linked first item = %#v, error = %v", linked[0], err)
	}
}

func TestTaskBatchIdempotencyOwnershipAndFailedRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-batch-controls?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TaskBatch{}, &model.TaskBatchItem{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	now := time.Now()
	newBatch := func(id string) (*model.TaskBatch, []model.TaskBatchItem) {
		batch := &model.TaskBatch{ID: id, UserID: "user-1", Mode: "image", Status: model.TaskBatchStatusQueued, RequestedCount: 2, WaitingCount: 2, IdempotencyKey: "same-request", RequestJSON: `{}`, CreatedAt: now, UpdatedAt: now}
		items := []model.TaskBatchItem{
			{ID: id + "-1", BatchID: id, Index: 0, Status: model.TaskBatchItemStatusFailed, Error: "upstream", CreatedAt: now, UpdatedAt: now},
			{ID: id + "-2", BatchID: id, Index: 1, Status: model.TaskBatchItemStatusWaiting, CreatedAt: now, UpdatedAt: now},
		}
		return batch, items
	}
	batch, items := newBatch("batch-controls")
	if err := repo.CreateTaskBatch(batch, items); err != nil {
		t.Fatal(err)
	}
	duplicate, duplicateItems := newBatch("batch-duplicate")
	if err := repo.CreateTaskBatch(duplicate, duplicateItems); !errors.Is(err, ErrTaskBatchIdempotency) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := repo.TaskBatchForUser("other-user", batch.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user read error = %v", err)
	}
	if err := repo.RetryFailedTaskBatchItemsForUser("user-1", batch.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := repo.TaskBatchItems(batch.ID)
	if err != nil || retried[0].Status != model.TaskBatchItemStatusWaiting || !retried[0].RetryRequested || retried[0].RetryCount != 1 {
		t.Fatalf("retried item = %#v, error = %v", retried[0], err)
	}
	if err := repo.CancelWaitingTaskBatchItemsForUser("user-1", batch.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := repo.TaskBatchItems(batch.ID)
	if err != nil || cancelled[0].Status != model.TaskBatchItemStatusCancelled || cancelled[1].Status != model.TaskBatchItemStatusCancelled {
		t.Fatalf("cancelled items = %#v, error = %v", cancelled, err)
	}
}
