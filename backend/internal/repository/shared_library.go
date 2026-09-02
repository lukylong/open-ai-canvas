package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) UpdateUserSharedLibraryAccess(userID string, enabled bool, event *model.AdminAuditEvent) (*model.User, error) {
	var user model.User
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Model(&user).Updates(map[string]any{"shared_library_enabled": enabled, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		user.SharedLibraryEnabled = enabled
		return tx.Create(event).Error
	})
	return &user, err
}

func (r *Repository) SharedAssetSeriesList() ([]model.SharedAssetSeries, error) {
	var rows []model.SharedAssetSeries
	err := r.db.Where("status = ?", model.SharedAssetSeriesReady).Order("updated_at desc").Find(&rows).Error
	return rows, err
}

func (r *Repository) SharedAssetSeries(id string) (*model.SharedAssetSeries, error) {
	var row model.SharedAssetSeries
	if err := r.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SaveSharedAssetSeries(row *model.SharedAssetSeries) error {
	return r.db.Save(row).Error
}

func (r *Repository) SharedAssets(seriesID string) ([]model.SharedAsset, error) {
	var rows []model.SharedAsset
	query := r.db.Where("status = ?", model.SharedAssetReady)
	if seriesID != "" {
		query = query.Where("series_id = ?", seriesID)
	}
	err := query.Order("updated_at desc").Find(&rows).Error
	return rows, err
}

func (r *Repository) SharedAsset(id string) (*model.SharedAsset, error) {
	var row model.SharedAsset
	if err := r.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SaveSharedAsset(row *model.SharedAsset) error { return r.db.Save(row).Error }

type ProjectSharedAssetRecord struct {
	Link  model.ProjectSharedAssetLink
	Asset model.SharedAsset
}

func (r *Repository) LinkProjectSharedAsset(link *model.ProjectSharedAssetLink) error {
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "shared_asset_id"}}, DoUpdates: clause.AssignmentColumns([]string{"version", "updated_at"})}).Create(link).Error
}

func (r *Repository) ProjectSharedAssets(projectID string) ([]ProjectSharedAssetRecord, error) {
	if !r.db.Migrator().HasTable(&model.ProjectSharedAssetLink{}) {
		return []ProjectSharedAssetRecord{}, nil
	}
	var links []model.ProjectSharedAssetLink
	if err := r.db.Where("project_id = ?", projectID).Order("created_at asc").Find(&links).Error; err != nil {
		return nil, err
	}
	result := make([]ProjectSharedAssetRecord, 0, len(links))
	for _, link := range links {
		asset, err := r.SharedAsset(link.SharedAssetID)
		if err != nil {
			return nil, err
		}
		result = append(result, ProjectSharedAssetRecord{Link: link, Asset: *asset})
	}
	return result, nil
}

func (r *Repository) DeleteProjectSharedAsset(projectID, sharedAssetID string) error {
	return r.db.Delete(&model.ProjectSharedAssetLink{}, "project_id = ? AND shared_asset_id = ?", projectID, sharedAssetID).Error
}

func (r *Repository) CreateSharedUploadBatch(batch *model.SharedAssetUploadBatch, items []model.SharedAssetUploadItem, series *model.SharedAssetSeries) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if series != nil {
			if err := tx.Create(series).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			return tx.Create(&items).Error
		}
		return nil
	})
}

func (r *Repository) SharedUploadBatchForUser(userID string, id string) (*model.SharedAssetUploadBatch, error) {
	var row model.SharedAssetUploadBatch
	if err := r.db.First(&row, "id = ? AND owner_user_id = ?", id, userID).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SharedUploadBatch(id string) (*model.SharedAssetUploadBatch, error) {
	var row model.SharedAssetUploadBatch
	if err := r.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SaveSharedUploadBatch(row *model.SharedAssetUploadBatch) error {
	return r.db.Save(row).Error
}

func (r *Repository) SharedUploadItems(batchID string) ([]model.SharedAssetUploadItem, error) {
	var rows []model.SharedAssetUploadItem
	err := r.db.Where("batch_id = ?", batchID).Order("created_at asc").Find(&rows).Error
	return rows, err
}

func (r *Repository) SharedUploadItemForUser(userID string, batchID string, itemID string) (*model.SharedAssetUploadItem, error) {
	var row model.SharedAssetUploadItem
	err := r.db.Table("shared_asset_upload_items AS item").
		Joins("JOIN shared_asset_upload_batches AS batch ON batch.id = item.batch_id").
		Where("item.id = ? AND item.batch_id = ? AND batch.owner_user_id = ?", itemID, batchID, userID).
		Select("item.*").First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SharedUploadItem(batchID string, clientID string) (*model.SharedAssetUploadItem, error) {
	var row model.SharedAssetUploadItem
	if err := r.db.First(&row, "batch_id = ? AND client_id = ?", batchID, clientID).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) SaveSharedUploadItem(row *model.SharedAssetUploadItem) error {
	return r.db.Save(row).Error
}

func (r *Repository) ClaimNextSharedZIPBatch(owner string, leaseDuration time.Duration) (*model.SharedAssetUploadBatch, error) {
	now := time.Now()
	var row model.SharedAssetUploadBatch
	err := r.db.Transaction(func(tx *gorm.DB) error {
		available := "mode = ? AND (status = ? OR ((status = ? OR status = ?) AND (lease_expires_at IS NULL OR lease_expires_at <= ?))) AND next_attempt_at <= ?"
		args := []any{"zip", model.SharedBatchQueued, model.SharedBatchExtracting, model.SharedBatchImporting, now, now}
		query := tx.Where(available, args...).Order("next_attempt_at asc, created_at asc").Limit(1)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		found := query.Find(&row)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		claim := tx.Model(&model.SharedAssetUploadBatch{}).Where("id = ?", row.ID)
		if r.Dialect() != "postgres" {
			claim = claim.Where(available, args...)
		}
		expires := now.Add(2 * time.Minute)
		updated := claim.Updates(map[string]any{
			"status": model.SharedBatchExtracting, "attempts": gorm.Expr("attempts + 1"),
			"lease_owner": owner, "lease_expires_at": expires, "heartbeat_at": now, "updated_at": now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			row = model.SharedAssetUploadBatch{}
			return nil
		}
		return tx.First(&row, "id = ?", row.ID).Error
	})
	if err != nil || row.ID == "" {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) HeartbeatSharedUploadBatch(id string, owner string, status model.SharedAssetUploadBatchStatus, leaseDuration time.Duration) error {
	now := time.Now()
	result := r.db.Model(&model.SharedAssetUploadBatch{}).Where("id = ? AND lease_owner = ?", id, owner).Updates(map[string]any{
		"status": status, "heartbeat_at": now, "lease_expires_at": now.Add(leaseDuration), "updated_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("共享素材上传任务租约已失效")
	}
	return nil
}

func (r *Repository) FinishSharedUploadBatch(id string, owner string, updates map[string]any) error {
	updates["lease_owner"] = ""
	updates["lease_expires_at"] = nil
	updates["heartbeat_at"] = nil
	updates["updated_at"] = time.Now()
	result := r.db.Model(&model.SharedAssetUploadBatch{}).Where("id = ? AND lease_owner = ?", id, owner).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("共享素材上传任务租约已失效")
	}
	return nil
}

func (r *Repository) CommitSharedAsset(item *model.SharedAssetUploadItem, asset *model.SharedAsset, seriesID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.SharedAsset
		err := tx.First(&existing, "upload_item_id = ?", item.ID).Error
		if err == nil {
			item.AssetID = existing.ID
			item.Status = model.SharedItemReady
			return tx.Save(item).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		item.AssetID = asset.ID
		item.Status = model.SharedItemReady
		item.Error = ""
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return tx.Model(&model.SharedAssetSeries{}).Where("id = ?", seriesID).Updates(map[string]any{
			"cover_resource_id": gorm.Expr("CASE WHEN cover_resource_id = '' THEN ? ELSE cover_resource_id END", asset.ResourceID),
			"updated_at":        time.Now(),
		}).Error
	})
}

func (r *Repository) RecountSharedUploadBatch(batchID string) (ready int64, skipped int64, failed int64, bytes int64, err error) {
	type counts struct {
		Ready   int64
		Skipped int64
		Failed  int64
		Bytes   int64
	}
	var value counts
	err = r.db.Model(&model.SharedAssetUploadItem{}).Where("batch_id = ?", batchID).Select(
		"SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS ready, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS skipped, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS failed, COALESCE(SUM(actual_size), 0) AS bytes",
		model.SharedItemReady, model.SharedItemSkipped, model.SharedItemFailed,
	).Scan(&value).Error
	return value.Ready, value.Skipped, value.Failed, value.Bytes, err
}

func (r *Repository) CleanupExpiredSharedUploadRows(before time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var batchIDs []string
		if err := tx.Model(&model.SharedAssetUploadBatch{}).
			Where("status IN ? AND updated_at < ?", []model.SharedAssetUploadBatchStatus{model.SharedBatchPreparing, model.SharedBatchUploading, model.SharedBatchCancelled}, before).
			Pluck("id", &batchIDs).Error; err != nil || len(batchIDs) == 0 {
			return err
		}
		if err := tx.Where("batch_id IN ?", batchIDs).Delete(&model.SharedAssetUploadItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", batchIDs).Delete(&model.SharedAssetUploadBatch{}).Error
	})
}
