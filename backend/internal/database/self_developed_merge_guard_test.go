package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestSelfDevelopedMigrationHistoryRemainsAppendOnly(t *testing.T) {
	want := []struct{ name, checksum string }{
		{"baseline_gorm_schema", "sha256:open-ai-canvas-schema-v1-20260830"},
		{"schema_migrations_applied_at_index", "sha256:schema-migrations-applied-at-index-v2-20260830"},
		{"asset_taxonomy_candidate_identity", "sha256:asset-taxonomy-candidate-identity-v3-20260831-r1"},
		{"shared_asset_library", "sha256:shared-asset-library-v4-20260902"},
		{"shared_asset_storage_scope", "sha256:shared-asset-storage-scope-v5-20260903"},
		{"legacy_zq_asset_client_payload", "sha256:legacy-zq-asset-client-payload-v6-20260903"},
		{"shared_asset_series_hierarchy", "sha256:shared-asset-series-hierarchy-v7-20260904"},
	}
	if len(schemaMigrations) < len(want) {
		t.Fatal("existing migrations were removed")
	}
	for index, original := range want {
		got := schemaMigrations[index]
		if got.version != int64(index+1) || got.name != original.name || got.checksum != original.checksum {
			t.Fatalf("published migration %d was overwritten: %#v", index+1, got)
		}
	}
}

func TestExistingDatabaseCopyPreservesCustomDataAfterRepeatedMigration(t *testing.T) {
	directory := t.TempDir()
	open := func(path string) *gorm.DB {
		db, err := Open(Config{Driver: "sqlite", DSN: path})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}
	original := open(filepath.Join(directory, "original.sqlite"))
	if err := MigrateSchema(original); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&model.User{ID: "retained-owner", Username: "retained-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true},
		&model.Resource{ID: "retained-resource", UserID: "retained-owner", Size: 128, SourceSystem: model.SharedLibraryResourceSourceSystem},
		&model.SharedAssetSeries{ID: "retained-root", Name: "共享根分类", OwnerUserID: "retained-owner", CoverResourceID: "retained-resource", Status: model.SharedAssetSeriesReady},
		&model.SharedAssetSeries{ID: "retained-child", Name: "共享子分类", ParentID: "retained-root", OwnerUserID: "retained-owner", Status: model.SharedAssetSeriesReady},
		&model.SharedAsset{ID: "retained-asset", SeriesID: "retained-child", ResourceID: "retained-resource", UploaderUserID: "retained-owner", UploadItemID: "retained-upload", Version: 7, Status: model.SharedAssetReady},
	} {
		if err := original.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	queries := []string{
		"SELECT * FROM schema_migrations ORDER BY version",
		"SELECT * FROM users ORDER BY id",
		"SELECT * FROM resources ORDER BY id",
		"SELECT * FROM shared_asset_series ORDER BY id",
		"SELECT * FROM shared_assets ORDER BY id",
	}
	snapshot := func(db *gorm.DB) string {
		var all []any
		for _, query := range queries {
			var rows []map[string]any
			if err := db.Raw(query).Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			all = append(all, rows)
		}
		encoded, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	before := snapshot(original)
	copyPath := filepath.Join(directory, "copy.sqlite")
	if err := original.Exec("VACUUM INTO ?", copyPath).Error; err != nil {
		t.Fatal(err)
	}
	copied := open(copyPath)
	for count := 0; count < 2; count++ {
		if err := MigrateSchema(copied); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot(copied) != before || snapshot(original) != before {
		t.Fatal("migration rewrote existing records, identity, ownership, cover, version or migration history")
	}
	var integrity string
	if err := copied.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil || integrity != "ok" {
		t.Fatalf("database copy integrity=%q error=%v", integrity, err)
	}
}
