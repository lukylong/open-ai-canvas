package zqmigration

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestImportPlatformStorageCreatesEncryptedSettingAndPreservesIt(t *testing.T) {
	target, err := gorm.Open(sqlite.Open("file:zq-platform-storage?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(target); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := target.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	migrator := New(nil, target, dataDir, COSConfig{
		Bucket: "bucket-1250000000", Region: "ap-guangzhou",
		Domain:      "https://bucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		AccessKeyID: "secret-id", AccessKeySecret: "secret-value",
	})
	created, err := migrator.ImportPlatformStorage(PlatformStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "created" || created.Provider != "tencent" || created.PathPrefix != "canvas" || created.CDNBaseURL != "" || !created.HasAccessKeyID || !created.HasAccessKeySecret {
		t.Fatalf("created platform storage = %#v", created)
	}
	var stored model.SystemSetting
	if err := target.First(&stored, "key = ?", platformOSSSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.ValueJSON, "secret-value") || !strings.Contains(stored.ValueJSON, "enc:v1:") {
		t.Fatalf("stored secret was not encrypted: %s", stored.ValueJSON)
	}
	originalJSON := stored.ValueJSON

	migrator.cos = COSConfig{Bucket: "changed-bucket", Region: "ap-shanghai", AccessKeyID: "changed-id", AccessKeySecret: "changed-secret"}
	preserved, err := migrator.ImportPlatformStorage(PlatformStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Status != "preserved" || preserved.Bucket != created.Bucket {
		t.Fatalf("preserved platform storage = %#v", preserved)
	}
	if err := target.First(&stored, "key = ?", platformOSSSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ValueJSON != originalJSON {
		t.Fatal("idempotent import overwrote the existing platform storage setting")
	}
}

func TestImportPlatformStorageCanReplaceOnlyWhenExplicit(t *testing.T) {
	target, err := gorm.Open(sqlite.Open("file:zq-platform-storage-replace?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(target); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := target.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	migrator := New(nil, target, t.TempDir(), COSConfig{Bucket: "first", Region: "ap-guangzhou", AccessKeyID: "id-1", AccessKeySecret: "secret-1"})
	if _, err := migrator.ImportPlatformStorage(PlatformStorageOptions{}); err != nil {
		t.Fatal(err)
	}
	migrator.cos = COSConfig{Bucket: "second", Region: "ap-shanghai", AccessKeyID: "id-2", AccessKeySecret: "secret-2", PublicRead: true, Domain: "https://second.cos.ap-shanghai.myqcloud.com"}
	replaced, err := migrator.ImportPlatformStorage(PlatformStorageOptions{Replace: true, PathPrefix: "/new-canvas/"})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Status != "updated" || replaced.Bucket != "second" || replaced.PathPrefix != "new-canvas" || replaced.CDNBaseURL == "" {
		t.Fatalf("replaced platform storage = %#v", replaced)
	}
}

func TestImportPlatformStorageRequiresActiveAdminAndCredentials(t *testing.T) {
	target, err := gorm.Open(sqlite.Open("file:zq-platform-storage-validation?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(target); err != nil {
		t.Fatal(err)
	}
	migrator := New(nil, target, t.TempDir(), COSConfig{Bucket: "bucket", Region: "ap-guangzhou", AccessKeyID: "id", AccessKeySecret: "secret"})
	if _, err := migrator.ImportPlatformStorage(PlatformStorageOptions{}); err == nil || !strings.Contains(err.Error(), "启用管理员") {
		t.Fatalf("missing admin error = %v", err)
	}
	admin := model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := target.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	migrator.cos.AccessKeySecret = ""
	if _, err := migrator.ImportPlatformStorage(PlatformStorageOptions{}); err == nil || !strings.Contains(err.Error(), "QCLOUD_COS_SECRET_KEY") {
		t.Fatalf("missing secret error = %v", err)
	}
}
