package service

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminGeneratedContentIncludesUserAndFullTaskDetails(t *testing.T) {
	svc, db, _ := newAdminContentTestService(t)
	now := time.Now()
	admin := model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	creator := model.User{ID: "creator", Username: "creator", DisplayName: "创作者", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]model.User{admin, creator}).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "task-1", UserID: creator.ID, Type: "image", Status: model.TaskStatusSucceeded, Prompt: "电影海报", InputJSON: `{"size":"16:9"}`, ResultJSON: `{"content":"asset://resource-1"}`, Model: "image-v1", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	page, err := svc.AdminGeneratedTasks(&admin, AdminGeneratedContentQuery{UserID: creator.ID, Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Tasks) != 1 {
		t.Fatalf("page = %#v", page)
	}
	item := page.Tasks[0]
	if item.User.Username != creator.Username || item.Prompt != task.Prompt || item.InputJSON != "" || item.ResultJSON != "" {
		t.Fatalf("task item = %#v", item)
	}
	detail, err := svc.AdminGeneratedTask(&admin, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.InputJSON != task.InputJSON || detail.ResultJSON != task.ResultJSON || detail.User.Username != creator.Username {
		t.Fatalf("task detail = %#v", detail)
	}
	if _, err := svc.AdminGeneratedTasks(&creator, AdminGeneratedContentQuery{}); err == nil {
		t.Fatal("AdminGeneratedTasks() allowed a non-admin")
	}
}

func TestAdminGeneratedResourcePreviewKeepsOwnerBoundary(t *testing.T) {
	svc, db, dataDir := newAdminContentTestService(t)
	now := time.Now()
	admin := model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	creator := model.User{ID: "creator", Username: "creator", DisplayName: "创作者", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]model.User{admin, creator}).Error; err != nil {
		t.Fatal(err)
	}
	objectKey := "creator/output.png"
	filePath := filepath.Join(dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("generated-image")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "resource-1", UserID: creator.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey, MimeType: "image/png", Size: int64(len(content)), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}

	page, err := svc.AdminGeneratedResources(&admin, AdminGeneratedContentQuery{UserID: creator.ID, Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Resources[0].PreviewURL != "/api/admin/generated-content/resources/resource-1/file" || page.Resources[0].User.Username != "creator" {
		t.Fatalf("resource page = %#v", page)
	}
	delivery, err := svc.PrepareAdminGeneratedResourceDelivery(&admin, resource.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer delivery.Stream.Body.Close()
	actual, err := io.ReadAll(delivery.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(content) {
		t.Fatalf("preview = %q, want %q", actual, content)
	}
	if _, err := svc.PrepareAdminGeneratedResourceDelivery(&creator, resource.ID, ""); err == nil {
		t.Fatal("PrepareAdminGeneratedResourceDelivery() allowed a non-admin")
	}
}

func newAdminContentTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Task{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	return &Service{repo: repository.New(db), dataDir: dataDir}, db, dataDir
}
