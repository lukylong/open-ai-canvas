package service

import (
	"strings"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDirectorProposalRequiresExplicitSelectionBeforeSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:director-proposal?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	userID := "director-user"
	canvasID := "director-canvas"
	if err := db.Create(&model.User{ID: userID, Username: "director", DisplayName: "Director"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CanvasProject{ID: canvasID, UserID: userID, Title: "Film"}).Error; err != nil {
		t.Fatal(err)
	}

	source := "女孩在雨中停下，回头说：别再跟着我。男孩收起伞。"
	proposal, err := svc.CreateDirectorPromptProposal(userID, CreateDirectorPromptProposalRequest{ProjectID: canvasID, SourceText: source})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "awaiting_selection" || len(proposal.Candidates) != 2 {
		t.Fatalf("proposal = %#v", proposal)
	}
	var tasks int64
	if err := db.Model(&model.Task{}).Count(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("proposal created %d tasks before selection", tasks)
	}
	if _, _, err := svc.selectedDirectorPrompt(userID, proposal.ID, canvasID, source); err == nil {
		t.Fatal("unselected proposal should not resolve")
	}
	selected, err := svc.SelectDirectorPromptProposal(userID, proposal.ID, SelectDirectorPromptProposalRequest{CandidateKey: "visual"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SelectedKey != "visual" || selected.Status != "selected" {
		t.Fatalf("selected = %#v", selected)
	}
	_, prompt, err := svc.selectedDirectorPrompt(userID, proposal.ID, canvasID, source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "九列表格") || !strings.Contains(prompt, source) {
		t.Fatalf("selected prompt missing production contract: %s", prompt)
	}
}
