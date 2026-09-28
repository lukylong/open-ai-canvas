package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSaveTaskCompletionWithOutputsIsAtomic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:generated-output-atomic?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.GeneratedMediaOutput{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	if err := db.Create(&model.Task{ID: "task-1", UserID: "user-1", Type: "canvas_image", Status: model.TaskStatusRunning, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	finished := model.Task{ID: "task-1", UserID: "user-1", Type: "canvas_image", Status: model.TaskStatusSucceeded, CreatedAt: now, CompletedAt: &now}
	first := model.GeneratedMediaOutput{TaskID: "task-1", ResourceID: "image-1", UserID: "user-1", Kind: "image", Model: "model-1", CreatedAt: now}
	if err := repo.SaveTaskCompletionWithOutputs(&finished, model.TaskStatusRunning, nil, nil, nil, []model.GeneratedMediaOutput{first, first}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.GeneratedMediaOutput{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("created outputs = %d, err = %v; want one unique output", count, err)
	}
	second := first
	second.ResourceID = "image-2"
	if err := repo.SaveTaskCompletionWithOutputs(&finished, model.TaskStatusRunning, nil, nil, nil, []model.GeneratedMediaOutput{second}); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("second completion error = %v; want state conflict", err)
	}
	if err := db.Model(&model.GeneratedMediaOutput{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("outputs after rejected completion = %d, err = %v; want one", count, err)
	}
}

func TestGenerationTaskOverviewDoesNotConfuseTasksWithOutputs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	tasks := []model.Task{
		{ID: "one", UserID: "a", Type: "canvas_image", Status: model.TaskStatusSucceeded, CreatedAt: start},
		{ID: "two", UserID: "a", Type: "canvas_video", Status: model.TaskStatusFailed, CreatedAt: start.Add(time.Hour)},
		{ID: "three", UserID: "b", Type: "canvas_image", Status: model.TaskStatusSucceeded, CreatedAt: start.Add(2 * time.Hour)},
		{ID: "not-media", UserID: "c", Type: "canvas_text", Status: model.TaskStatusSucceeded, CreatedAt: start},
		{ID: "end-excluded", UserID: "d", Type: "canvas_image", Status: model.TaskStatusSucceeded, CreatedAt: start.Add(24 * time.Hour)},
	}
	if err = db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	got, err := New(db).GenerationTaskOverview(start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 3 || got.Succeeded != 2 || got.Failed != 1 || got.Creators != 2 || len(got.Trend) != 1 || got.Trend[0].Day != "2026-09-22" {
		t.Fatalf("wrong summary: %+v", got)
	}
}
