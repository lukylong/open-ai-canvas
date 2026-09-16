package repository

import (
	"errors"
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
	if err := db.AutoMigrate(&model.DistributionOutbox{}, &model.DistributionPublication{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.DistributionPublication{ID: "publication-1", Status: model.DistributionPublicationPending}).Error; err != nil {
		t.Fatal(err)
	}
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

func TestTerminalDistributionFailureStopsUntilExplicitRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DistributionOutbox{}, &model.DistributionPublication{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	repo := New(db)
	pub := model.DistributionPublication{ID: "pub", UserID: "owner", Status: model.DistributionPublicationFailed}
	item := model.DistributionOutbox{ID: "outbox", PublicationID: pub.ID, Status: model.DistributionOutboxFailed, Attempts: 174, NextAttemptAt: &now}
	if err := repo.CreateDistributionPublication(&pub, &item); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimDistributionOutbox(now.Add(time.Hour)); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("legacy terminal failure was reclaimed", err)
	}
	if _, err := repo.RetryDistributionPublication("owner", pub.ID, now); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimDistributionOutbox(now)
	if err != nil || claimed.Attempts != 1 {
		t.Fatal("manual retry did not reset attempts", err)
	}
	if err := repo.FailDistributionOutbox(item.ID, pub.ID, "temporary", now, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimDistributionOutbox(now); err != nil {
		t.Fatal("transient retry was disabled", err)
	}
	if err := repo.FailDistributionOutbox(item.ID, pub.ID, "terminal", now, true); err != nil {
		t.Fatal(err)
	}
	var terminalItem model.DistributionOutbox
	if err := db.First(&terminalItem, "id = ?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if terminalItem.Status != model.DistributionOutboxStopped || terminalItem.NextAttemptAt != nil {
		t.Fatal("terminal outbox still scheduled")
	}
	if _, err := repo.ClaimDistributionOutbox(now.Add(24 * time.Hour)); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("stopped job was reclaimed", err)
	}
	t.Log("legacy terminal failure skipped; transient retries preserved; stopped job has no next retry; explicit retry resets attempts")
}
