package database

import (
	"infinite-canvas/backend/internal/model"
	"testing"
)

func TestPersonalSeriesMigrationFromV7(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:personal-series-migration?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.PersonalAssetMembership{}, &model.PersonalAssetSeries{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("version = ?", 8).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: "existing", UserID: "owner", PayloadJSON: `{"metadata":{"taskId":"preserved"}}`}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.PersonalAssetMembership{}, &model.PersonalAssetSeries{}} {
		if !db.Migrator().HasTable(row) {
			t.Fatal("new series table missing")
		}
	}
	var got model.Asset
	if err := db.First(&got, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PayloadJSON != asset.PayloadJSON {
		t.Fatal("migration rewrote asset payload")
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal("migration not idempotent", err)
	}
}
