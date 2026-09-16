package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"infinite-canvas/backend/internal/model"
	"time"
)

func (r *Repository) PersonalSeriesTransaction(userID string, apply func(*Repository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Serialize hierarchy and membership writes for one account, including empty trees.
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		return apply(New(tx))
	})
}

func (r *Repository) PersonalSeries(userID string) ([]model.PersonalAssetSeries, error) {
	rows := []model.PersonalAssetSeries{}
	err := r.db.Where("user_id = ?", userID).Order("created_at, id").Find(&rows).Error
	return rows, err
}

func (r *Repository) PersonalMemberships(userID string) ([]model.PersonalAssetMembership, error) {
	rows := []model.PersonalAssetMembership{}
	err := r.db.Table("personal_asset_memberships m").Select("m.*").
		Joins("JOIN assets a ON a.id=m.asset_id AND a.user_id=m.user_id").
		Where("m.user_id = ? AND a.status <> ?", userID, model.AssetVersionStatusArchived).Order("m.asset_id").Scan(&rows).Error
	return rows, err
}

func (r *Repository) SavePersonalSeries(row *model.PersonalAssetSeries) error {
	stamp, err := r.PersonalSeriesWriteTime(row.UserID)
	if err != nil {
		return err
	}
	row.UpdatedAt = stamp
	return r.db.Session(&gorm.Session{SkipHooks: true}).Save(row).Error
}

// Millisecond versions are sent to distribution. Keep ordering even when two
// management actions finish within one millisecond, without touching Asset.
func (r *Repository) PersonalSeriesWriteTime(userID string) (time.Time, error) {
	stamp := time.Now().Truncate(time.Millisecond)
	series, err := r.PersonalSeries(userID)
	if err != nil {
		return stamp, err
	}
	members, err := r.PersonalMemberships(userID)
	if err != nil {
		return stamp, err
	}
	for _, row := range series {
		if !stamp.After(row.UpdatedAt) {
			stamp = row.UpdatedAt.Add(time.Millisecond)
		}
	}
	for _, row := range members {
		if !stamp.After(row.UpdatedAt) {
			stamp = row.UpdatedAt.Add(time.Millisecond)
		}
	}
	return stamp, nil
}

func (r *Repository) RemovePersonalSeries(userID, id string) error {
	stamp, err := r.PersonalSeriesWriteTime(userID)
	if err != nil {
		return err
	}
	if err := r.db.Model(&model.PersonalAssetMembership{}).Where("user_id = ? AND series_id = ?", userID, id).Updates(map[string]any{"series_id": "", "updated_at": stamp}).Error; err != nil {
		return err
	}
	return r.db.Where("user_id = ? AND id = ?", userID, id).Delete(&model.PersonalAssetSeries{}).Error
}

func (r *Repository) MovePersonalMembers(userID, seriesID string, assetIDs []string) error {
	stamp, err := r.PersonalSeriesWriteTime(userID)
	if err != nil {
		return err
	}
	current, err := r.PersonalMemberships(userID)
	if err != nil {
		return err
	}
	byAsset := map[string]string{}
	for _, member := range current {
		byAsset[member.AssetID] = member.SeriesID
	}
	rows := make([]model.PersonalAssetMembership, 0, len(assetIDs))
	for _, id := range assetIDs {
		if previous, ok := byAsset[id]; ok && previous == seriesID {
			continue
		}
		rows = append(rows, model.PersonalAssetMembership{AssetID: id, UserID: userID, SeriesID: seriesID, UpdatedAt: stamp})
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "asset_id"}}, DoUpdates: clause.AssignmentColumns([]string{"series_id", "updated_at"})}).CreateInBatches(rows, 100).Error
}

func (r *Repository) PersonalMembership(userID, assetID string) (*model.PersonalAssetMembership, error) {
	var row model.PersonalAssetMembership
	err := r.db.First(&row, "user_id = ? AND asset_id = ?", userID, assetID).Error
	return &row, err
}

func (r *Repository) PersonalSeriesForAsset(userID, assetID string) (*model.PersonalAssetSeries, error) {
	var row model.PersonalAssetSeries
	err := r.db.Table("personal_asset_series s").Select("s.*").
		Joins("JOIN personal_asset_memberships m ON m.series_id=s.id AND m.user_id=s.user_id").
		Where("s.user_id = ? AND m.asset_id = ?", userID, assetID).First(&row).Error
	return &row, err
}
