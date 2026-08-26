package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClaimDistributionOutboxRecoversStaleProcessingItem(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:distribution-recovery?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DistributionOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	item := model.DistributionOutbox{ID: "outbox-1", PublicationID: "publication-1", Status: model.DistributionOutboxProcessing, Attempts: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-3 * time.Minute)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := New(db).ClaimDistributionOutbox(now)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != item.ID || claimed.Status != model.DistributionOutboxProcessing || claimed.Attempts != 2 {
		t.Fatalf("claimed item = %#v", claimed)
	}
}
