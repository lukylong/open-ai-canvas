package model

import "time"

type MigrationRunStatus string
type MigrationEntityStatus string
type DistributionPublicationStatus string
type DistributionOutboxStatus string

const (
	MigrationRunRunning   MigrationRunStatus = "running"
	MigrationRunSucceeded MigrationRunStatus = "succeeded"
	MigrationRunFailed    MigrationRunStatus = "failed"

	MigrationEntityImported MigrationEntityStatus = "imported"
	MigrationEntityMerged   MigrationEntityStatus = "merged"
	MigrationEntityConflict MigrationEntityStatus = "conflict"
	MigrationEntitySkipped  MigrationEntityStatus = "skipped"

	DistributionPublicationPending   DistributionPublicationStatus = "pending"
	DistributionPublicationPublished DistributionPublicationStatus = "published"
	DistributionPublicationFailed    DistributionPublicationStatus = "failed"
	DistributionPublicationCancelled DistributionPublicationStatus = "cancelled"

	DistributionOutboxPending    DistributionOutboxStatus = "pending"
	DistributionOutboxProcessing DistributionOutboxStatus = "processing"
	DistributionOutboxDelivered  DistributionOutboxStatus = "delivered"
	DistributionOutboxFailed     DistributionOutboxStatus = "failed"
)

type MigrationRun struct {
	ID            string             `json:"id" gorm:"primaryKey;size:36"`
	SourceSystem  string             `json:"sourceSystem" gorm:"index;size:32"`
	Mode          string             `json:"mode" gorm:"size:24"`
	Status        MigrationRunStatus `json:"status" gorm:"index;size:24"`
	Watermark     time.Time          `json:"watermark" gorm:"index"`
	WatermarkID   string             `json:"watermarkId" gorm:"size:80"`
	ScannedCount  int64              `json:"scannedCount"`
	ImportedCount int64              `json:"importedCount"`
	MergedCount   int64              `json:"mergedCount"`
	ConflictCount int64              `json:"conflictCount"`
	SkippedCount  int64              `json:"skippedCount"`
	Error         string             `json:"error,omitempty" gorm:"type:text"`
	StartedAt     time.Time          `json:"startedAt"`
	FinishedAt    *time.Time         `json:"finishedAt,omitempty"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

// MigrationEntityMap is the idempotency boundary for a source row.
type MigrationEntityMap struct {
	ID            string                `json:"id" gorm:"primaryKey;size:36"`
	SourceSystem  string                `json:"sourceSystem" gorm:"size:32;uniqueIndex:idx_migration_source_entity,priority:1"`
	EntityType    string                `json:"entityType" gorm:"size:32;uniqueIndex:idx_migration_source_entity,priority:2"`
	SourceID      string                `json:"sourceId" gorm:"size:80;uniqueIndex:idx_migration_source_entity,priority:3"`
	TargetID      string                `json:"targetId,omitempty" gorm:"index;size:80"`
	Status        MigrationEntityStatus `json:"status" gorm:"index;size:24"`
	SourceUpdated time.Time             `json:"sourceUpdated" gorm:"index"`
	Checksum      string                `json:"checksum" gorm:"size:64"`
	DetailsJSON   string                `json:"detailsJson,omitempty" gorm:"type:text"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
}

type MigrationConflict struct {
	ID           string     `json:"id" gorm:"primaryKey;size:36"`
	RunID        string     `json:"runId" gorm:"index;size:36"`
	SourceSystem string     `json:"sourceSystem" gorm:"index;size:32"`
	EntityType   string     `json:"entityType" gorm:"index;size:32"`
	SourceID     string     `json:"sourceId" gorm:"index;size:80"`
	Reason       string     `json:"reason" gorm:"size:120"`
	DetailsJSON  string     `json:"detailsJson" gorm:"type:text"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty" gorm:"index"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type DistributionPublication struct {
	ID             string                        `json:"id" gorm:"primaryKey;size:36"`
	UserID         string                        `json:"userId" gorm:"index;size:36"`
	AssetID        string                        `json:"assetId" gorm:"index;size:80"`
	AssetVersionID string                        `json:"assetVersionId" gorm:"index;size:36"`
	Target         string                        `json:"target" gorm:"index;size:48"`
	ExternalID     string                        `json:"externalId,omitempty" gorm:"size:160"`
	Status         DistributionPublicationStatus `json:"status" gorm:"index;size:24"`
	PayloadJSON    string                        `json:"payloadJson" gorm:"type:text"`
	LastError      string                        `json:"lastError,omitempty" gorm:"type:text"`
	PublishedAt    *time.Time                    `json:"publishedAt,omitempty" gorm:"index"`
	CreatedAt      time.Time                     `json:"createdAt"`
	UpdatedAt      time.Time                     `json:"updatedAt"`
}

type DistributionOutbox struct {
	ID             string                   `json:"id" gorm:"primaryKey;size:36"`
	PublicationID  string                   `json:"publicationId" gorm:"uniqueIndex;size:36"`
	EventType      string                   `json:"eventType" gorm:"size:64"`
	IdempotencyKey string                   `json:"idempotencyKey" gorm:"uniqueIndex;size:160"`
	PayloadJSON    string                   `json:"payloadJson" gorm:"type:text"`
	Status         DistributionOutboxStatus `json:"status" gorm:"index;size:24"`
	Attempts       int                      `json:"attempts"`
	NextAttemptAt  *time.Time               `json:"nextAttemptAt,omitempty" gorm:"index"`
	LastError      string                   `json:"lastError,omitempty" gorm:"type:text"`
	DeliveredAt    *time.Time               `json:"deliveredAt,omitempty"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
}

// DirectorPromptProposal keeps the two-phase professional-director decision.
// Creating a proposal never starts generation; only a selected proposal can be
// consumed by a storyboard session.
type DirectorPromptProposal struct {
	ID                string     `json:"id" gorm:"primaryKey;size:36"`
	UserID            string     `json:"userId" gorm:"index;size:36"`
	ProjectID         string     `json:"projectId,omitempty" gorm:"index;size:36"`
	SourceText        string     `json:"sourceText" gorm:"type:text"`
	CandidatesJSON    string     `json:"-" gorm:"type:text"`
	RecommendedKey    string     `json:"recommendedKey" gorm:"size:16"`
	SelectedKey       string     `json:"selectedKey,omitempty" gorm:"size:16"`
	Status            string     `json:"status" gorm:"index;size:32"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	SelectedAt        *time.Time `json:"selectedAt,omitempty"`
	ConsumedAt        *time.Time `json:"consumedAt,omitempty"`
	ConsumedSessionID string     `json:"consumedSessionId,omitempty" gorm:"index;size:36"`
}
