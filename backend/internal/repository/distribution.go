package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDistributionOutboxUnavailable = errors.New("distribution outbox unavailable")

func (r *Repository) CreateDistributionPublication(publication *model.DistributionPublication, outbox *model.DistributionOutbox) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(publication).Error; err != nil {
			return err
		}
		return tx.Create(outbox).Error
	})
}

func (r *Repository) DistributionPublications(userID string, limit int) ([]model.DistributionPublication, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var items []model.DistributionPublication
	return items, r.db.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&items).Error
}

func (r *Repository) DistributionPublicationForUser(userID string, id string) (*model.DistributionPublication, error) {
	var item model.DistributionPublication
	if err := r.db.First(&item, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) DistributionPublicationByIdempotencyKey(userID string, key string) (*model.DistributionPublication, error) {
	var item model.DistributionPublication
	err := r.db.Table("distribution_publications").Select("distribution_publications.*").
		Joins("JOIN distribution_outboxes ON distribution_outboxes.publication_id = distribution_publications.id").
		Where("distribution_publications.user_id = ? AND distribution_outboxes.idempotency_key = ?", userID, key).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) RetryDistributionPublication(userID string, id string, now time.Time) (*model.DistributionPublication, error) {
	var publication model.DistributionPublication
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&publication, "id = ? AND user_id = ?", id, userID).Error; err != nil {
			return err
		}
		if publication.Status != model.DistributionPublicationFailed {
			return ErrDistributionOutboxUnavailable
		}
		if err := tx.Model(&publication).Updates(map[string]any{"status": model.DistributionPublicationPending, "last_error": "", "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.DistributionOutbox{}).Where("publication_id = ?", publication.ID).Updates(map[string]any{"status": model.DistributionOutboxPending, "next_attempt_at": &now, "last_error": "", "updated_at": now}).Error
	})
	if err == nil {
		err = r.db.First(&publication, "id = ?", id).Error
	}
	return &publication, err
}

func (r *Repository) CancelDistributionPublication(userID string, id string, now time.Time) (*model.DistributionPublication, error) {
	var publication model.DistributionPublication
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&publication, "id = ? AND user_id = ?", id, userID).Error; err != nil {
			return err
		}
		if publication.Status != model.DistributionPublicationPending {
			return ErrDistributionOutboxUnavailable
		}
		if err := tx.Model(&publication).Updates(map[string]any{"status": model.DistributionPublicationCancelled, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.DistributionOutbox{}).Where("publication_id = ? AND status IN ?", publication.ID, []model.DistributionOutboxStatus{model.DistributionOutboxPending, model.DistributionOutboxFailed}).Updates(map[string]any{"status": model.DistributionOutboxDelivered, "delivered_at": &now, "updated_at": now}).Error
	})
	if err == nil {
		err = r.db.First(&publication, "id = ?", id).Error
	}
	return &publication, err
}

func (r *Repository) ClaimDistributionOutbox(now time.Time) (*model.DistributionOutbox, error) {
	var item model.DistributionOutbox
	err := r.db.Transaction(func(tx *gorm.DB) error {
		staleBefore := now.Add(-2 * time.Minute)
		eligible := "(status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at <= ?)"
		states := []model.DistributionOutboxStatus{model.DistributionOutboxPending, model.DistributionOutboxFailed}
		query := tx.Where(eligible, states, now, model.DistributionOutboxProcessing, staleBefore).Order("created_at").Limit(1)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		result := query.Find(&item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		updated := tx.Model(&model.DistributionOutbox{}).Where("id = ? AND ("+eligible+")", item.ID, states, now, model.DistributionOutboxProcessing, staleBefore).Updates(map[string]any{"status": model.DistributionOutboxProcessing, "attempts": gorm.Expr("attempts + 1"), "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrDistributionOutboxUnavailable
		}
		return tx.First(&item, "id = ?", item.ID).Error
	})
	return &item, err
}

func (r *Repository) CompleteDistributionOutbox(itemID string, publicationID string, externalID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.DistributionOutbox{}).Where("id = ?", itemID).Updates(map[string]any{"status": model.DistributionOutboxDelivered, "delivered_at": &now, "last_error": "", "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.DistributionPublication{}).Where("id = ?", publicationID).Updates(map[string]any{"status": model.DistributionPublicationPublished, "external_id": externalID, "published_at": &now, "last_error": "", "updated_at": now}).Error
	})
}

func (r *Repository) FailDistributionOutbox(itemID string, publicationID string, message string, retryAt time.Time, terminal bool) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		outboxStatus := model.DistributionOutboxFailed
		publicationStatus := model.DistributionPublicationPending
		if terminal {
			publicationStatus = model.DistributionPublicationFailed
		}
		if err := tx.Model(&model.DistributionOutbox{}).Where("id = ?", itemID).Updates(map[string]any{"status": outboxStatus, "last_error": message, "next_attempt_at": &retryAt, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&model.DistributionPublication{}).Where("id = ?", publicationID).Updates(map[string]any{"status": publicationStatus, "last_error": message, "updated_at": time.Now()}).Error
	})
}
