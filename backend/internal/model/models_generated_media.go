package model

import "time"

// GeneratedMediaOutput records a media resource created while completing a task.
// It is not a generic Resource inventory row: uploads and imports never enter it.
type GeneratedMediaOutput struct {
	TaskID     string    `gorm:"primaryKey;size:36"`
	ResourceID string    `gorm:"primaryKey;size:36"`
	UserID     string    `gorm:"index;size:36"`
	Kind       string    `gorm:"index;size:24"`
	Model      string    `gorm:"index;size:120"`
	CreatedAt  time.Time `gorm:"index"`
}
