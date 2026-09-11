package service

import (
	"path/filepath"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestAPICallLogSurvivesAuxiliaryWriteFailures(t *testing.T) {
	for _, target := range []string{"tasks", "billing_orders", "api_call_logs"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			db, err := database.Open(database.Config{Driver: "sqlite", DSN: filepath.Join(dir, "fixture.sqlite")})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			for _, row := range []any{
				&model.User{ID: "user", Username: "user", Status: model.UserStatusActive, Role: model.UserRoleUser},
				&model.Task{ID: "task", UserID: "user", Type: "canvas_image", Status: model.TaskStatusRunning},
				&model.BillingOrder{ID: "billing", UserID: "user", TaskID: "task"},
			} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			action := "UPDATE"
			if target == "api_call_logs" {
				action = "INSERT"
			}
			if err := db.Exec("CREATE TRIGGER fail_write BEFORE " + action + " ON " + target + " BEGIN SELECT RAISE(ABORT, 'injected write failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			svc := New(repository.New(db), dir)
			entry := model.ApiCallLog{ID: "upstream-call", UserID: "user", TaskID: "task", BillingOrderID: "billing", ProviderRequestID: "provider-task", Capability: "image", RequestKind: "create", Status: model.ApiCallStatusSucceeded, StatusCode: 200}
			err = svc.LogAPICall(entry)
			var count int64
			if queryErr := db.Model(&model.ApiCallLog{}).Where("id = ?", entry.ID).Count(&count).Error; queryErr != nil {
				t.Fatal(queryErr)
			}
			if target == "api_call_logs" {
				if err == nil || count != 0 {
					t.Fatalf("real log write failure was swallowed: error=%v count=%d", err, count)
				}
			} else if err != nil || count != 1 {
				t.Fatalf("auxiliary update lost upstream evidence: error=%v count=%d", err, count)
			}
		})
	}
}
