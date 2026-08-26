package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminGeneratedContentQueriesFilterAcrossUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin-content-repository?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Task{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	users := []model.User{
		{ID: "user-a", Username: "alice", DisplayName: "爱丽丝", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "user-b", Username: "bob", DisplayName: "鲍勃", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Task{
		{ID: "task-a", UserID: "user-a", Type: "image", Status: model.TaskStatusSucceeded, Prompt: "海边电影海报", Model: "image-v1", CreatedAt: now.Add(time.Second)},
		{ID: "task-b", UserID: "user-b", Type: "video", Status: model.TaskStatusFailed, Prompt: "城市夜景", Model: "video-v1", CreatedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Resource{
		{ID: "resource-a", UserID: "user-a", Kind: "image", Status: model.ResourceStatusReady, SourceSystem: "zq-media-studio", ObjectKey: "alice/poster.png", CreatedAt: now.Add(time.Second), UpdatedAt: now},
		{ID: "resource-b", UserID: "user-b", Kind: "video", Status: model.ResourceStatusReady, ObjectKey: "bob/movie.mp4", CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}

	repo := New(db)
	tasks, total, err := repo.AdminGeneratedTasks(AdminGeneratedContentFilter{Keyword: "alice", Kind: "image"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(tasks) != 1 || tasks[0].ID != "task-a" || tasks[0].Prompt != "海边电影海报" {
		t.Fatalf("tasks = %#v, total = %d", tasks, total)
	}
	resources, total, err := repo.AdminGeneratedResources(AdminGeneratedContentFilter{UserID: "user-a", SourceSystem: "zq-media-studio"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(resources) != 1 || resources[0].ID != "resource-a" {
		t.Fatalf("resources = %#v, total = %d", resources, total)
	}
	resources, total, err = repo.AdminGeneratedResources(AdminGeneratedContentFilter{SourceSystem: "canvas"}, 20, 0)
	if err != nil || total != 1 || len(resources) != 1 || resources[0].ID != "resource-b" {
		t.Fatalf("canvas resources = %#v, total = %d, error = %v", resources, total, err)
	}
}
