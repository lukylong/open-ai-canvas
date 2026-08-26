package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserAssetListsHideArchivedAssets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:asset-visibility?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, asset := range []model.Asset{
		{ID: "asset-ready", UserID: "user-1", Kind: "image", Status: model.AssetVersionStatusConfirmed, Title: "Ready", PayloadJSON: `{"id":"asset-ready"}`, CreatedAt: now, UpdatedAt: now},
		{ID: "asset-archived", UserID: "user-1", Kind: "image", Status: model.AssetVersionStatusArchived, Title: "Archived", PayloadJSON: `{"id":"asset-archived"}`, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	assets, err := repo.Assets("user-1")
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := repo.AssetSummaries("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].ID != "asset-ready" || len(summaries) != 1 || summaries[0].ID != "asset-ready" {
		t.Fatalf("assets = %#v, summaries = %#v", assets, summaries)
	}
}
