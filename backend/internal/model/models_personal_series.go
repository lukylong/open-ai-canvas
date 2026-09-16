package model

import "time"

// PersonalAssetSeries organizes private assets without changing generation lineage.
type PersonalAssetSeries struct {
	ID           string    `json:"id" gorm:"primaryKey;size:36"`
	UserID       string    `json:"-" gorm:"index;size:36"`
	Name         string    `json:"name" gorm:"size:80"`
	ParentID     string    `json:"parentId" gorm:"index;size:36"`
	CoverAssetID string    `json:"coverAssetId" gorm:"size:80"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type PersonalAssetMembership struct {
	AssetID   string    `json:"assetId" gorm:"primaryKey;size:80"`
	UserID    string    `json:"-" gorm:"index;size:36"`
	SeriesID  string    `json:"seriesId" gorm:"index;size:36"`
	UpdatedAt time.Time `json:"updatedAt"`
}
