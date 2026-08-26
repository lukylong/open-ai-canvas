package zqmigration

import "time"

type SourceBase struct {
	ID                string    `gorm:"column:id"`
	IsDeleted         bool      `gorm:"column:is_deleted"`
	SysCreateDatetime time.Time `gorm:"column:sys_create_datetime"`
	SysUpdateDatetime time.Time `gorm:"column:sys_update_datetime"`
	SysCreatorID      string    `gorm:"column:sys_creator_id"`
}

type SourceAccount struct {
	SourceBase
	Username     string     `gorm:"column:username"`
	Email        string     `gorm:"column:email"`
	PasswordHash string     `gorm:"column:password_hash"`
	DisplayName  string     `gorm:"column:display_name"`
	AvatarURL    string     `gorm:"column:avatar_url"`
	Status       string     `gorm:"column:status"`
	Source       string     `gorm:"column:source"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
}

func (SourceAccount) TableName() string { return "accounts" }

type SourceInvitationCode struct {
	SourceBase
	Code      string     `gorm:"column:code"`
	Label     string     `gorm:"column:label"`
	MaxUses   int        `gorm:"column:max_uses"`
	UsedCount int        `gorm:"column:used_count"`
	ExpiresAt *time.Time `gorm:"column:expires_at"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	IsActive  bool       `gorm:"column:is_active"`
}

func (SourceInvitationCode) TableName() string { return "invitation_codes" }

type SourceInvitationUsage struct {
	SourceBase
	InvitationCodeID string    `gorm:"column:invitation_code_id"`
	AccountID        string    `gorm:"column:account_id"`
	UsedAt           time.Time `gorm:"column:used_at"`
}

func (SourceInvitationUsage) TableName() string { return "invitation_code_usages" }

type SourceAsset struct {
	SourceBase
	AccountID        string     `gorm:"column:account_id"`
	GenerationTaskID string     `gorm:"column:generation_task_id"`
	Kind             string     `gorm:"column:kind"`
	Source           string     `gorm:"column:source"`
	Title            string     `gorm:"column:title"`
	URL              string     `gorm:"column:url"`
	StorageKey       string     `gorm:"column:storage_key"`
	MimeType         string     `gorm:"column:mime_type"`
	Width            int        `gorm:"column:width"`
	Height           int        `gorm:"column:height"`
	Ordinal          int        `gorm:"column:ordinal"`
	Visibility       string     `gorm:"column:visibility"`
	PublishedAt      *time.Time `gorm:"column:published_at"`
	AssetMetadata    []byte     `gorm:"column:asset_metadata"`
}

func (SourceAsset) TableName() string { return "assets" }

type SourceGenerationTask struct {
	ID                string `gorm:"column:id"`
	BatchID           string `gorm:"column:batch_id"`
	BatchStartOrdinal int    `gorm:"column:batch_start_ordinal"`
}

func (SourceGenerationTask) TableName() string { return "generation_tasks" }

type SourceVoiceProfile struct {
	SourceBase
	AccountID       string     `gorm:"column:account_id"`
	SampleAssetID   string     `gorm:"column:sample_asset_id"`
	Name            string     `gorm:"column:name"`
	Description     string     `gorm:"column:description"`
	ReferenceText   string     `gorm:"column:reference_text"`
	Language        string     `gorm:"column:language"`
	Status          string     `gorm:"column:status"`
	Visibility      string     `gorm:"column:visibility"`
	TrainingMode    string     `gorm:"column:training_mode"`
	UsageCount      int        `gorm:"column:usage_count"`
	PublishedAt     *time.Time `gorm:"column:published_at"`
	ErrorMessage    string     `gorm:"column:error_message"`
	ProfileMetadata []byte     `gorm:"column:profile_metadata"`
}

func (SourceVoiceProfile) TableName() string { return "voice_profiles" }
