package database

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestAssetRepresentationPartialTaskRoleIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:asset-representation-partial-index?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, item := range []model.AssetRepresentation{
		{ID: "representation-import-1", AssetVersionID: "version-1", Role: "original", CreatedAt: now},
		{ID: "representation-import-2", AssetVersionID: "version-2", Role: "original", CreatedAt: now},
	} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("insert task-less representation: %v", err)
		}
	}
	first := model.AssetRepresentation{ID: "representation-task-1", TaskID: "task-1", AssetVersionID: "version-3", Role: "original", CreatedAt: now}
	duplicate := model.AssetRepresentation{ID: "representation-task-2", TaskID: "task-1", AssetVersionID: "version-4", Role: "original", CreatedAt: now}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate non-empty task/role should be rejected")
	}
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_asset_representations_task_role").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(definition), "where task_id <> ''") {
		t.Fatalf("index definition = %q", definition)
	}
}

func TestAssetIDColumnsUseSharedLimit(t *testing.T) {
	tests := []struct {
		value any
		field string
	}{
		{value: &model.Asset{}, field: "ID"},
		{value: &model.ProjectAssetLink{}, field: "AssetID"},
		{value: &model.ProjectAssetCandidate{}, field: "ResolvedAssetID"},
		{value: &model.AssetVersion{}, field: "AssetID"},
	}

	for _, test := range tests {
		parsed, err := schema.Parse(test.value, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse schema: %v", err)
		}
		field := parsed.LookUpField(test.field)
		if field == nil {
			t.Fatalf("field %s not found in %s", test.field, parsed.Table)
		}
		if field.Size != model.AssetIDMaxLength {
			t.Fatalf("%s.%s size = %d, want %d", parsed.Table, field.DBName, field.Size, model.AssetIDMaxLength)
		}
	}
}

func TestPostgresAssetIDMigrationsCoverEveryAssetIDColumn(t *testing.T) {
	want := map[string]bool{
		"assets.id":                                  false,
		"project_asset_links.asset_id":               false,
		"project_asset_candidates.resolved_asset_id": false,
		"asset_versions.asset_id":                    false,
	}
	for _, migration := range assetIDColumnMigrations {
		key := migration.table + "." + migration.column
		if _, exists := want[key]; !exists {
			t.Fatalf("unexpected asset ID migration %s", key)
		}
		if !strings.Contains(migration.statement, fmt.Sprintf("varchar(%d)", model.AssetIDMaxLength)) {
			t.Fatalf("asset ID migration %s does not use limit %d", key, model.AssetIDMaxLength)
		}
		want[key] = true
	}
	for key, covered := range want {
		if !covered {
			t.Fatalf("missing asset ID migration %s", key)
		}
	}
}
