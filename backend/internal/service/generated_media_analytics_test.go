package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPersistGeneratedMediaCapturesOnlyNewOutputResources(t *testing.T) {
	svc := newResourceTestService(t)
	result, outputs, err := svc.persistGeneratedMediaResultWithOutputs("user-1", map[string]interface{}{
		"outputs": []interface{}{
			map[string]interface{}{"dataUrl": "data:image/png;base64,YQ=="},
			map[string]interface{}{"dataUrl": "data:video/mp4;base64,Yg=="},
		},
		"inputReference": map[string]interface{}{"resourceId": "uploaded-source"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 || outputs[0].Kind != "image" || outputs[1].Kind != "video" || outputs[0].Status != model.ResourceStatusReady || outputs[1].Status != model.ResourceStatusReady {
		t.Fatalf("new output resources = %+v; want two ready image/video resources", outputs)
	}
	if result["inputReference"].(map[string]interface{})["resourceId"] != "uploaded-source" {
		t.Fatal("existing input resource reference was modified")
	}
}

func TestGlobalGeneratedMediaAnalyticsUsesStoredOutputs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:generated-media-overview?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.GeneratedMediaOutput{}, &model.Task{}, &model.UserDailyActivity{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE schema_migrations (version integer primary key, applied_at datetime)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", 9, time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatal(err)
	}
	previousDay := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	selectedDay := time.Date(2026, 9, 22, 16, 30, 0, 0, time.UTC)
	items := []model.GeneratedMediaOutput{
		{TaskID: "task-1", ResourceID: "image-1", UserID: "user-1", Kind: "image", Model: "model-a", CreatedAt: selectedDay},
		{TaskID: "task-1", ResourceID: "image-2", UserID: "user-1", Kind: "image", Model: "model-a", CreatedAt: selectedDay},
		{TaskID: "task-2", ResourceID: "video-1", UserID: "user-1", Kind: "video", Model: "model-b", CreatedAt: selectedDay},
		{TaskID: "task-0", ResourceID: "image-0", UserID: "user-2", Kind: "image", Model: "model-a", CreatedAt: previousDay},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Task{
		{ID: "task-1", UserID: "user-1", Type: "canvas_image", CreatedAt: selectedDay},
		{ID: "task-2", UserID: "user-1", Type: "canvas_video", CreatedAt: selectedDay},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	got, err := svc.GlobalGeneratedMediaAnalytics("2026-09-23", "2026-09-23")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CoverageComplete || got.ImageOutputs != 2 || got.VideoOutputs != 1 || got.GenerationTasks != 2 || got.ActiveUsers != 1 {
		t.Fatalf("overview = %+v; want 2 images, 1 video, 2 tasks, 1 active user and full coverage", got)
	}
	if len(got.Trend) != 2 || got.Trend[0].Day != "2026-09-23" || len(got.Models) != 2 {
		t.Fatalf("trend/models = %+v / %+v; want Shanghai 09-23 buckets", got.Trend, got.Models)
	}
	if _, err := svc.GlobalGeneratedMediaAnalytics("2026-09-31", "2026-09-31"); err == nil {
		t.Fatal("invalid calendar day should be rejected")
	}
}
