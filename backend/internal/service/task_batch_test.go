package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateTaskBatchRejectsCountAboveRuntimePolicyBeforeCreatingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-batch-limit?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskBatch{}, &model.TaskBatchItem{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	_, err = svc.CreateTaskBatch("user-1", CreateTaskBatchRequest{
		Count: 1001, IdempotencyKey: "too-many", Task: CreateTaskRequest{Prompt: "test", LogicalModelID: "model-1", Type: "canvas_image", Input: map[string]any{"mode": "image"}},
	})
	if err == nil || !strings.Contains(err.Error(), "1 到 1000") {
		t.Fatalf("count 1001 error = %v", err)
	}
	var count int64
	if err := db.Model(&model.TaskBatch{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("created batches = %d, error = %v", count, err)
	}
}

func TestTaskBatchItemStatusProjection(t *testing.T) {
	for status, want := range map[model.TaskStatus]model.TaskBatchItemStatus{
		model.TaskStatusQueued: model.TaskBatchItemStatusQueued, model.TaskStatusRunning: model.TaskBatchItemStatusRunning,
		model.TaskStatusSucceeded: model.TaskBatchItemStatusSucceeded, model.TaskStatusFailed: model.TaskBatchItemStatusFailed,
		model.TaskStatusCancelled: model.TaskBatchItemStatusCancelled,
	} {
		if got := taskBatchItemStatusForTask(status); got != want {
			t.Fatalf("status %s -> %s, want %s", status, got, want)
		}
	}
}

func TestTaskBatchPromotionHonorsActiveTaskLimitAndKeepsRemainingItemsWaiting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-batch-promotion?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, openErr := db.DB(); openErr == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{
		Key:       featureAvailabilitySettingKey,
		ValueJSON: `{"shortDramaEnabled":true,"taskCenterEnabled":true,"creditsEnabled":false,"customChannelsEnabled":true}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	capabilityConfig := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceOpenAIImage), "image-test")
	capabilityConfigJSON, err := json.Marshal(capabilityConfig)
	if err != nil {
		t.Fatal(err)
	}
	capabilitySpec, err := CapabilitySpecFromModelCapabilityConfig(capabilityConfig, "image")
	if err != nil {
		t.Fatal(err)
	}
	capabilitySpecJSON, err := json.Marshal(capabilitySpec)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	channel := model.ModelChannel{ID: "batch-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Batch test", BaseURL: "https://example.com/v1", CreatedAt: now, UpdatedAt: now}
	channelModel := model.ChannelModel{
		ID: "batch-channel-model", ChannelID: channel.ID, ModelKey: "image-test", ProviderModelKey: "image-test", DisplayName: "Batch image",
		Capability: "image", Protocol: model.ChannelInterfaceOpenAIImage, BillingMode: "fixed_request", Enabled: true,
		CapabilityConfigJSON: string(capabilityConfigJSON), CapabilityVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	revision := model.LogicalModelRevision{ID: "batch-revision", LogicalModelID: "batch-logical-model", Version: 1, CapabilitySpecJSON: string(capabilitySpecJSON), DefaultOptionsJSON: `{}`, CreatedAt: now}
	logicalModel := model.LogicalModel{
		ID: revision.LogicalModelID, Code: "batch-image", Name: "Batch image", Capability: "image", Enabled: true,
		ActiveRevisionID: revision.ID, PricePolicy: "unified", BillingMode: "fixed_request", UnitPriceMicrocredits: 1, CreatedAt: now, UpdatedAt: now,
	}
	route := model.LogicalModelRoute{ID: "batch-route", LogicalModelRevisionID: revision.ID, ChannelModelID: channelModel.ID, Enabled: true, Priority: 1, Weight: 100, CreatedAt: now, UpdatedAt: now}
	for _, value := range []any{&channel, &channelModel, &logicalModel, &revision, &route} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := New(repository.New(db), t.TempDir())
	detail, err := svc.CreateTaskBatch("user-1", CreateTaskBatchRequest{
		Count: 6, IdempotencyKey: "promotion-limit", Task: CreateTaskRequest{
			Prompt: "test", LogicalModelID: logicalModel.ID, Type: "canvas_image", Operation: "image",
			Input: map[string]any{"mode": "image", "prompt": "test", "config": map[string]any{"count": "1"}, "capabilityOptions": map[string]any{"count": 1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.RequestedCount != 6 || detail.Batch.WaitingCount != 6 {
		t.Fatalf("created batch = %#v", detail.Batch)
	}
	if err := svc.promoteTaskBatchItems(); err != nil {
		t.Fatal(err)
	}

	var tasks []model.Task
	if err := db.Where("batch_id = ?", detail.Batch.ID).Order("batch_index asc").Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != defaultRuntimePolicy().Task.ActiveTaskLimit {
		t.Fatalf("promoted tasks = %d, want %d", len(tasks), defaultRuntimePolicy().Task.ActiveTaskLimit)
	}
	updated, err := svc.TaskBatchDetail("user-1", detail.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Batch.QueuedCount != defaultRuntimePolicy().Task.ActiveTaskLimit || updated.Batch.WaitingCount != 1 {
		t.Fatalf("updated batch = %#v", updated.Batch)
	}
	for index, task := range tasks {
		if task.BatchItemID == nil || task.BatchID != detail.Batch.ID || task.BatchIndex != index {
			t.Fatalf("task %d linkage = %#v", index, task)
		}
	}
}
