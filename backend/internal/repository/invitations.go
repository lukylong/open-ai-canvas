package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvitationInvalid = errors.New("invitation code is invalid")

func (r *Repository) InvitationCodeByHash(codeHash string) (*model.InvitationCode, error) {
	var invite model.InvitationCode
	if err := r.db.First(&invite, "code_hash = ?", codeHash).Error; err != nil {
		return nil, err
	}
	return &invite, nil
}

func (r *Repository) InvitationCodes() ([]model.InvitationCode, error) {
	var invites []model.InvitationCode
	return invites, r.db.Order("created_at DESC").Find(&invites).Error
}

func (r *Repository) CreateInvitationCodeWithAudit(invite *model.InvitationCode, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(invite).Error; err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) RevokeInvitationCode(id string, revokedAt time.Time) (*model.InvitationCode, error) {
	result := r.db.Model(&model.InvitationCode{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{"revoked_at": &revokedAt, "updated_at": revokedAt})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	var invite model.InvitationCode
	if err := r.db.First(&invite, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &invite, nil
}

// CreateUserWithInvitation consumes the invite and creates the account in one transaction.
func (r *Repository) CreateUserWithInvitation(user *model.User, codeHash string, usageID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var invite model.InvitationCode
		query := tx.Where("code_hash = ?", codeHash)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&invite).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvitationInvalid
			}
			return err
		}
		if invite.RevokedAt != nil || invite.ExpiresAt != nil && !invite.ExpiresAt.After(now) || invite.MaxUses > 0 && invite.UsedCount >= invite.MaxUses {
			return ErrInvitationInvalid
		}
		updated := tx.Model(&model.InvitationCode{}).
			Where("id = ? AND revoked_at IS NULL AND used_count = ? AND (expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR used_count < max_uses)", invite.ID, invite.UsedCount, now).
			Updates(map[string]any{"used_count": gorm.Expr("used_count + 1"), "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrInvitationInvalid
		}
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		usage := model.InvitationCodeUsage{ID: usageID, InvitationCodeID: invite.ID, UserID: user.ID, CreatedAt: now}
		return tx.Create(&usage).Error
	})
}

func (r *Repository) UpdateUserPasswordAndRevokeSessions(userID string, passwordHash string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{"password_hash": passwordHash, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Delete(&model.AuthSession{}, "user_id = ?", userID).Error
	})
}
