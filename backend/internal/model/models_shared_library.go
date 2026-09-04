package model

import "time"

type SharedAssetSeriesStatus string
type SharedAssetStatus string
type SharedAssetUploadBatchStatus string
type SharedAssetUploadItemStatus string

const (
	SharedLibraryResourceSourceSystem = "shared-library"

	SharedAssetSeriesPreparing SharedAssetSeriesStatus = "preparing"
	SharedAssetSeriesReady     SharedAssetSeriesStatus = "ready"
	SharedAssetSeriesArchived  SharedAssetSeriesStatus = "archived"

	SharedAssetPending  SharedAssetStatus = "pending"
	SharedAssetReady    SharedAssetStatus = "ready"
	SharedAssetArchived SharedAssetStatus = "archived"

	SharedBatchPreparing           SharedAssetUploadBatchStatus = "preparing"
	SharedBatchUploading           SharedAssetUploadBatchStatus = "uploading"
	SharedBatchQueued              SharedAssetUploadBatchStatus = "queued"
	SharedBatchExtracting          SharedAssetUploadBatchStatus = "extracting"
	SharedBatchImporting           SharedAssetUploadBatchStatus = "importing"
	SharedBatchCompleted           SharedAssetUploadBatchStatus = "completed"
	SharedBatchCompletedWithErrors SharedAssetUploadBatchStatus = "completed_with_errors"
	SharedBatchFailed              SharedAssetUploadBatchStatus = "failed"
	SharedBatchCancelled           SharedAssetUploadBatchStatus = "cancelled"

	SharedItemPending   SharedAssetUploadItemStatus = "pending"
	SharedItemUploading SharedAssetUploadItemStatus = "uploading"
	SharedItemVerifying SharedAssetUploadItemStatus = "verifying"
	SharedItemReady     SharedAssetUploadItemStatus = "ready"
	SharedItemSkipped   SharedAssetUploadItemStatus = "skipped"
	SharedItemFailed    SharedAssetUploadItemStatus = "failed"
)

type SharedAssetSeries struct {
	ID              string                  `json:"id" gorm:"primaryKey;size:36"`
	Name            string                  `json:"name" gorm:"size:160"`
	ParentID        string                  `json:"parentId,omitempty" gorm:"index;size:36"`
	OwnerUserID     string                  `json:"ownerUserId" gorm:"index;size:36"`
	CoverResourceID string                  `json:"coverResourceId,omitempty" gorm:"index;size:36"`
	Status          SharedAssetSeriesStatus `json:"status" gorm:"index;size:24"`
	CreatedAt       time.Time               `json:"createdAt"`
	UpdatedAt       time.Time               `json:"updatedAt" gorm:"index"`
}

type SharedAsset struct {
	ID                  string            `json:"id" gorm:"primaryKey;size:36"`
	SeriesID            string            `json:"seriesId" gorm:"index;size:36"`
	UploaderUserID      string            `json:"uploaderUserId" gorm:"index;size:36"`
	ResourceID          string            `json:"resourceId" gorm:"index;size:36"`
	ThumbnailResourceID string            `json:"thumbnailResourceId,omitempty" gorm:"index;size:36"`
	UploadItemID        string            `json:"uploadItemId" gorm:"uniqueIndex;size:36"`
	Title               string            `json:"title" gorm:"size:255"`
	MimeType            string            `json:"mimeType" gorm:"size:80"`
	Size                int64             `json:"size"`
	Width               int               `json:"width"`
	Height              int               `json:"height"`
	SHA256              string            `json:"sha256" gorm:"index;size:64"`
	Version             int               `json:"version" gorm:"not null;default:1"`
	Status              SharedAssetStatus `json:"status" gorm:"index;size:24"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt" gorm:"index"`
}

type SharedAssetUploadBatch struct {
	ID             string                       `json:"id" gorm:"primaryKey;size:36"`
	OwnerUserID    string                       `json:"ownerUserId" gorm:"index;size:36"`
	SeriesID       string                       `json:"seriesId" gorm:"index;size:36"`
	Mode           string                       `json:"mode" gorm:"size:16"`
	Status         SharedAssetUploadBatchStatus `json:"status" gorm:"index:idx_shared_batch_due,priority:1;size:32"`
	FileCount      int                          `json:"fileCount"`
	TotalBytes     int64                        `json:"totalBytes"`
	ReadyCount     int                          `json:"readyCount"`
	SkippedCount   int                          `json:"skippedCount"`
	FailedCount    int                          `json:"failedCount"`
	ProcessedBytes int64                        `json:"processedBytes"`
	Error          string                       `json:"error,omitempty" gorm:"type:text"`
	Attempts       int                          `json:"attempts"`
	NextAttemptAt  time.Time                    `json:"nextAttemptAt" gorm:"index:idx_shared_batch_due,priority:2"`
	LeaseOwner     string                       `json:"-" gorm:"index;size:120"`
	LeaseExpiresAt *time.Time                   `json:"-" gorm:"index"`
	HeartbeatAt    *time.Time                   `json:"-"`
	ArchivePath    string                       `json:"-" gorm:"type:text"`
	ArchiveExpires *time.Time                   `json:"-" gorm:"index"`
	CreatedAt      time.Time                    `json:"createdAt"`
	UpdatedAt      time.Time                    `json:"updatedAt" gorm:"index"`
}

type SharedAssetUploadItem struct {
	ID              string                      `json:"id" gorm:"primaryKey;size:36"`
	BatchID         string                      `json:"batchId" gorm:"uniqueIndex:idx_shared_upload_item_client,priority:1;index;size:36"`
	ClientID        string                      `json:"clientId" gorm:"uniqueIndex:idx_shared_upload_item_client,priority:2;size:160"`
	FileName        string                      `json:"fileName" gorm:"size:512"`
	DeclaredMime    string                      `json:"declaredMime,omitempty" gorm:"size:80"`
	ExpectedSize    int64                       `json:"expectedSize"`
	ExpectedSHA256  string                      `json:"expectedSha256,omitempty" gorm:"size:64"`
	ActualSize      int64                       `json:"actualSize"`
	ActualSHA256    string                      `json:"actualSha256,omitempty" gorm:"size:64"`
	Status          SharedAssetUploadItemStatus `json:"status" gorm:"index;size:24"`
	Error           string                      `json:"error,omitempty" gorm:"type:text"`
	StagingPath     string                      `json:"-" gorm:"type:text"`
	UploadTokenHash string                      `json:"-" gorm:"size:64"`
	UploadExpiresAt *time.Time                  `json:"uploadExpiresAt,omitempty" gorm:"index"`
	AssetID         string                      `json:"assetId,omitempty" gorm:"index;size:36"`
	CreatedAt       time.Time                   `json:"createdAt"`
	UpdatedAt       time.Time                   `json:"updatedAt"`
}

type ProjectSharedAssetLink struct {
	ID            string    `json:"id" gorm:"primaryKey;size:36"`
	ProjectID     string    `json:"projectId" gorm:"uniqueIndex:idx_project_shared_asset,priority:1;index;size:36"`
	SharedAssetID string    `json:"sharedAssetId" gorm:"uniqueIndex:idx_project_shared_asset,priority:2;index;size:36"`
	Version       int       `json:"version"`
	CreatedBy     string    `json:"createdBy" gorm:"index;size:36"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
