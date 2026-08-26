package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) DirectorPromptProposalForUser(userID string, id string) (*model.DirectorPromptProposal, error) {
	var proposal model.DirectorPromptProposal
	if err := r.db.First(&proposal, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &proposal, nil
}

func (r *Repository) SelectDirectorPromptProposal(userID string, id string, key string, now time.Time) (*model.DirectorPromptProposal, error) {
	result := r.db.Model(&model.DirectorPromptProposal{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, "awaiting_selection").
		Updates(map[string]any{"selected_key": key, "status": "selected", "selected_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.DirectorPromptProposalForUser(userID, id)
}

// ConsumeDirectorPromptProposal binds a selected decision to exactly one
// session. Replaying the same session ID is idempotent, while another session
// cannot reuse the decision.
func (r *Repository) ConsumeDirectorPromptProposal(userID string, id string, sessionID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.DirectorPromptProposal{}).
			Where("id = ? AND user_id = ? AND status = ? AND (consumed_session_id = '' OR consumed_session_id = ?)", id, userID, "selected", sessionID).
			Updates(map[string]any{"consumed_at": now, "consumed_session_id": sessionID, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrDuplicatedKey
		}
		return nil
	})
}

// ReleaseDirectorPromptProposal makes a reservation reusable only when the
// exact session that reserved it fails before a generation task is created.
func (r *Repository) ReleaseDirectorPromptProposal(userID string, id string, sessionID string, now time.Time) error {
	updated := r.db.Model(&model.DirectorPromptProposal{}).
		Where("id = ? AND user_id = ? AND consumed_session_id = ?", id, userID, sessionID).
		Updates(map[string]any{"consumed_at": nil, "consumed_session_id": "", "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
