package repository

import (
	"fmt"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminGeneratedTasksFilterCanonicalKinds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Task{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{{ID: "alice", Username: "alice", DisplayName: "测试甲"}, {ID: "bob", Username: "bob", DisplayName: "测试乙"}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tasks := []model.Task{
		{ID: "video-text", UserID: "alice", Type: "canvas_video", Operation: "text_to_video", Status: model.TaskStatusSucceeded, Prompt: "海边", Model: "comfy-h3-t2v"},
		{ID: "video-image", UserID: "alice", Type: "canvas_video", Operation: "image_to_video", Status: model.TaskStatusFailed, Prompt: "海边", Model: "comfy-h3-i2v"},
		{ID: "video-reference", UserID: "bob", Type: "canvas_video", Operation: "reference_to_video", Status: model.TaskStatusSucceeded, Prompt: "海边", Model: "comfy-h3-r2v"},
		{ID: "image", UserID: "alice", Type: "canvas_image", Operation: "image", Status: model.TaskStatusSucceeded, Prompt: "海边", Model: "image-model"},
		{ID: "text", UserID: "alice", Type: "canvas_text", Operation: "text", Status: model.TaskStatusSucceeded},
		{ID: "audio", UserID: "alice", Type: "canvas_audio", Operation: "text_to_speech", Status: model.TaskStatusSucceeded},
	}
	for i := range tasks {
		tasks[i].CreatedAt = now.Add(-time.Duration(i) * time.Second)
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	for _, tc := range []struct {
		name   string
		filter AdminGeneratedContentFilter
		ids    []string
	}{
		{"video", AdminGeneratedContentFilter{Kind: "video"}, []string{"video-text", "video-image", "video-reference"}},
		{"normalized_video", AdminGeneratedContentFilter{Kind: " VIDEO "}, []string{"video-text", "video-image", "video-reference"}},
		{"image", AdminGeneratedContentFilter{Kind: "image"}, []string{"image"}},
		{"text", AdminGeneratedContentFilter{Kind: "text"}, []string{"text"}},
		{"audio", AdminGeneratedContentFilter{Kind: "audio"}, []string{"audio"}},
		{"combined_success", AdminGeneratedContentFilter{Kind: "video", UserID: "alice", Status: "succeeded", Keyword: "海边"}, []string{"video-text"}},
		{"combined_failure", AdminGeneratedContentFilter{Kind: "video", UserID: "alice", Status: "failed", Keyword: "comfy-h3"}, []string{"video-image"}},
		{"username", AdminGeneratedContentFilter{Kind: "video", Keyword: "bob"}, []string{"video-reference"}},
		{"empty", AdminGeneratedContentFilter{Kind: "video", UserID: "bob", Status: "failed"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, total, err := repo.AdminGeneratedTasks(tc.filter, 20, 0)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(actual))
			for _, task := range actual {
				ids = append(ids, task.ID)
			}
			if total != int64(len(tc.ids)) || fmt.Sprint(ids) != fmt.Sprint(tc.ids) {
				t.Fatalf("filter=%+v: got ids=%v total=%d, want ids=%v", tc.filter, ids, total, tc.ids)
			}
		})
	}
	t.Run("filtered_pagination", func(t *testing.T) {
		items, total, err := repo.AdminGeneratedTasks(AdminGeneratedContentFilter{Kind: "video"}, 1, 1)
		if err != nil || total != 3 || len(items) != 1 || items[0].ID != "video-image" {
			t.Fatalf("filtered page: items=%v total=%d err=%v", items, total, err)
		}
	})
}
