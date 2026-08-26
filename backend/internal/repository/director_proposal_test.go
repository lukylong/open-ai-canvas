package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestConsumeDirectorPromptProposalIsSingleUseAndIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:director-proposal-repository?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DirectorPromptProposal{}); err != nil {
		t.Fatal(err)
	}
	proposal := model.DirectorPromptProposal{ID: "proposal-1", UserID: "user-1", Status: "selected", SelectedKey: "visual", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	if err := repo.ConsumeDirectorPromptProposal("user-1", proposal.ID, "session-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeDirectorPromptProposal("user-1", proposal.ID, "session-1", time.Now()); err != nil {
		t.Fatalf("same-session replay should be idempotent: %v", err)
	}
	if err := repo.ConsumeDirectorPromptProposal("user-1", proposal.ID, "session-2", time.Now()); !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("second session error = %v, want duplicated key", err)
	}
	if err := repo.ReleaseDirectorPromptProposal("user-1", proposal.ID, "session-2", time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign session release error = %v, want not found", err)
	}
	if err := repo.ReleaseDirectorPromptProposal("user-1", proposal.ID, "session-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeDirectorPromptProposal("user-1", proposal.ID, "session-2", time.Now()); err != nil {
		t.Fatalf("released proposal should be reusable: %v", err)
	}
}
