package database

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestMigrateSchemaRecordsAndValidatesVersion(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-version?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	status, err := ReadSchemaStatus(db)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.Current != CurrentSchemaVersion {
		t.Fatalf("unexpected schema status: %#v", status)
	}
	if !db.Migrator().HasIndex(&schemaMigration{}, "idx_schema_migrations_applied_at") {
		t.Fatal("schema migration v2 did not create the applied_at index")
	}
	if !db.Migrator().HasIndex(&model.ProjectAssetCandidate{}, "idx_project_asset_candidates_pending_identity") {
		t.Fatal("schema migration v3 did not create candidate identity index")
	}
	for _, table := range []any{&model.SharedAssetSeries{}, &model.SharedAsset{}, &model.SharedAssetUploadBatch{}, &model.SharedAssetUploadItem{}, &model.ProjectSharedAssetLink{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("schema migration v4 table missing: %T", table)
		}
	}
	if !db.Migrator().HasColumn(&model.User{}, "SharedLibraryEnabled") {
		t.Fatal("schema migration v4 did not add users.shared_library_enabled")
	}
	resource := model.Resource{ID: "shared-resource-v5", UserID: "user-1", Size: 123}
	asset := model.SharedAsset{ID: "shared-asset-v5", SeriesID: "series-1", UploaderUserID: "user-1", ResourceID: resource.ID, ThumbnailResourceID: resource.ID, UploadItemID: "item-v5"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSchemaV5(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&resource, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resource.SourceSystem != model.SharedLibraryResourceSourceSystem {
		t.Fatalf("schema migration v5 source_system = %q", resource.SourceSystem)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
}

func TestMigrateSchemaRejectsChecksumMismatch(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-checksum?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schemaMigration{}).Where("version = ?", CurrentSchemaVersion).Update("checksum", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil || !strings.Contains(err.Error(), "校验和不一致") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if err := RequireSchemaVersion(db); err == nil || !strings.Contains(err.Error(), "校验和不一致") {
		t.Fatalf("schema verification must reject checksum mismatch, got %v", err)
	}
}

func TestMigrateSchemaV3NormalizesLegacyAccessoryCategory(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-asset-taxonomy?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}, &model.ProjectAssetCandidate{}); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: "asset-1", UserID: "user-1", Kind: "image", Category: model.AssetCategory("accessory"), Title: "旧配饰"}
	candidate := model.ProjectAssetCandidate{ID: "candidate-1", ProjectID: "project-1", Name: "旧配饰候选", Category: model.AssetCategory("accessory"), Status: "pending_confirmation"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSchemaV3(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&asset, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, "id = ?", candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.Category != model.AssetCategoryProp || candidate.Category != model.AssetCategoryProp {
		t.Fatalf("legacy accessory categories = %q/%q, want prop/prop", asset.Category, candidate.Category)
	}
	if candidate.NameKey != model.AssetCandidateNameKey(candidate.Name) {
		t.Fatalf("candidate name key = %q", candidate.NameKey)
	}
}

func TestMigrateSchemaV6RepairsLegacyZQAssetClientPayload(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-zq-asset-payload?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Resource{}, &model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 3, 1, 2, 3, 0, time.UTC)
	resource := model.Resource{ID: "zqr_legacy", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Size: 77, Width: 2, Height: 2, CreatedAt: now, UpdatedAt: now}
	asset := model.Asset{ID: "zqa_legacy", UserID: "user-1", Kind: "reference", Category: model.AssetCategoryOther, Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: "zqv_legacy", Title: "旧参考图", PayloadJSON: `{"source":"upload","metadata":{"source_system":"zq-media-studio"}}`, CreatedAt: now, UpdatedAt: now}
	version := model.AssetVersion{ID: asset.PrimaryVersionID, AssetID: asset.ID, Version: 1, Status: model.AssetVersionStatusConfirmed, CreatedAt: now, UpdatedAt: now}
	representation := model.AssetRepresentation{ID: "zqp_legacy", AssetVersionID: version.ID, ResourceID: resource.ID, MediaType: "image", Role: "original", CreatedAt: now}
	for _, value := range []any{&resource, &asset, &version, &representation} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateSchemaV6(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&asset, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(asset.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok || data["storageKey"] != "resource:"+resource.ID || data["dataUrl"] != "/api/resources/"+resource.ID+"/file" {
		t.Fatalf("repaired payload data = %#v", payload["data"])
	}
	if asset.Kind != "image" || payload["id"] != asset.ID || payload["kind"] != "image" || payload["title"] != asset.Title {
		t.Fatalf("repaired asset = %#v, payload = %#v", asset, payload)
	}
	before := asset.PayloadJSON
	if err := migrateSchemaV6(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&asset, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.PayloadJSON != before {
		t.Fatal("migration v6 is not idempotent")
	}
}

func TestMigrateSchemaRollsBackFailedMigration(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-rollback?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	original := schemaMigrations
	schemaMigrations = append(append([]migration(nil), original...), migration{
		version:  CurrentSchemaVersion + 1,
		name:     "rollback_probe",
		checksum: "sha256:rollback-probe",
		apply: func(tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE migration_rollback_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				return err
			}
			return errors.New("forced migration failure")
		},
	})
	t.Cleanup(func() { schemaMigrations = original })

	if err := MigrateSchema(db); err == nil || !strings.Contains(err.Error(), "forced migration failure") {
		t.Fatalf("expected forced migration failure, got %v", err)
	}
	if db.Migrator().HasTable("migration_rollback_probe") {
		t.Fatal("failed migration left a partial table behind")
	}
	var count int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", CurrentSchemaVersion+1).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed migration was recorded: %d", count)
	}
}

func TestRequireSchemaVersionRejectsUninitializedDatabase(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-uninitialized?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireSchemaVersion(db); err == nil || !strings.Contains(err.Error(), "请先执行 migrate-schema up") {
		t.Fatalf("expected missing migration error, got %v", err)
	}
}
