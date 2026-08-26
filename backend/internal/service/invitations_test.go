package service

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInvitationRegistrationAndPasswordChange(t *testing.T) {
	t.Setenv("CANVAS_REGISTRATION_ENABLED", "false")
	db, err := gorm.Open(sqlite.Open("file:invite-auth?mode=memory&cache=shared"), &gorm.Config{})
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

	adminSession, err := svc.Register(RegisterRequest{Username: "admin_user", DisplayName: "Admin", Password: "password-123"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := dbUser(db, adminSession.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateRegistrationSetting(admin, RegistrationSettingRequest{Enabled: true}); err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(time.Hour)
	created, err := svc.CreateInvitationCode(admin, CreateInvitationCodeRequest{Label: "integration", MaxUses: 1, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if created.Code == "" || created.CodeHash == "" {
		t.Fatalf("created invite = %#v", created)
	}

	registered, err := svc.Register(RegisterRequest{Username: "invited_user", DisplayName: "Invited", Password: "password-456", InvitationCode: created.Code})
	if err != nil {
		t.Fatal(err)
	}
	if registered.User.Email != "" || registered.User.SourceSystem != "canvas" {
		t.Fatalf("registered user = %#v", registered.User)
	}
	if _, err := svc.Register(RegisterRequest{Username: "second_user", Password: "password-789", InvitationCode: created.Code}); err == nil {
		t.Fatal("used invite should be rejected")
	}

	user, err := dbUser(db, registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(user, ChangePasswordRequest{CurrentPassword: "password-456", NewPassword: "password-new-456"}); err != nil {
		t.Fatal(err)
	}
	var sessionCount int64
	if err := db.Model(&model.AuthSession{}).Where("user_id = ?", user.ID).Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("session count = %d, want 0", sessionCount)
	}
	if _, err := svc.Login(LoginRequest{Username: user.Username, Password: "password-456"}); err == nil {
		t.Fatal("old password should be rejected")
	}
	if _, err := svc.Login(LoginRequest{Username: user.Username, Password: "password-new-456"}); err != nil {
		t.Fatal(err)
	}

	stored, err := repository.New(db).InvitationCodeByHash(created.CodeHash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UsedCount != 1 {
		t.Fatalf("used count = %d, want 1", stored.UsedCount)
	}
}

func TestInvitationRegistrationRejectsMissingCode(t *testing.T) {
	t.Setenv("CANVAS_REGISTRATION_ENABLED", "true")
	db, err := gorm.Open(sqlite.Open("file:invite-missing?mode=memory&cache=shared"), &gorm.Config{})
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
	if _, err := svc.Register(RegisterRequest{Username: "admin_user", Password: "password-123"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Register(RegisterRequest{Username: "plain_user", Password: "password-456"})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != 400 {
		t.Fatalf("Register() error = %#v", err)
	}
}

func dbUser(db *gorm.DB, id string) (*model.User, error) {
	var user model.User
	return &user, db.First(&user, "id = ?", id).Error
}
