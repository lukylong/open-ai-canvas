package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type GeneratedMediaCount struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

type GeneratedMediaDay struct {
	Day   string `json:"day"`
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

type GeneratedMediaModel struct {
	Kind  string `json:"kind"`
	Model string `json:"model"`
	Count int64  `json:"count"`
}

type GenerationTaskDay struct {
	Day       string `json:"day"`
	Count     int64  `json:"count"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
	Creators  int64  `json:"creators"`
}

type GenerationTaskType struct {
	Model     string `json:"model"`
	Count     int64  `json:"count"`
	Succeeded int64  `json:"succeeded"`
}

type GenerationTaskSummary struct {
	Count     int64
	Succeeded int64
	Failed    int64
	Creators  int64
	Trend     []GenerationTaskDay  `gorm:"-"`
	Types     []GenerationTaskType `gorm:"-"`
}

// Task activity is a separate fact from persisted output counts.
func (r *Repository) GenerationTaskOverview(from, to time.Time) (*GenerationTaskSummary, error) {
	base := func() *gorm.DB {
		return r.db.Model(&model.Task{}).
			Where("created_at >= ? AND created_at < ?", from, to).
			Where("type IN ?", []string{"canvas_image", "canvas_video", "image_generate", "video_generate"})
	}
	result := &GenerationTaskSummary{Trend: []GenerationTaskDay{}, Types: []GenerationTaskType{}}
	counts := "COUNT(*) AS count, COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),0) AS succeeded, COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0) AS failed, COUNT(DISTINCT user_id) AS creators"
	if err := base().Select(counts).Scan(result).Error; err != nil {
		return nil, err
	}
	day := "strftime('%Y-%m-%d', created_at, '+8 hours')"
	if r.db.Dialector.Name() == "postgres" {
		day = "to_char(created_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD')"
	}
	if err := base().Select(day + " AS day, " + counts).Group(day).Order("day ASC").Scan(&result.Trend).Error; err != nil {
		return nil, err
	}
	if err := base().Select("type AS model, COUNT(*) AS count, COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),0) AS succeeded").Group("type").Order("count DESC, type ASC").Scan(&result.Types).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) GeneratedMediaStats(from, to time.Time) ([]GeneratedMediaCount, []GeneratedMediaDay, []GeneratedMediaModel, error) {
	base := func() *gorm.DB {
		return r.db.Model(&model.GeneratedMediaOutput{}).Where("created_at >= ? AND created_at < ?", from, to)
	}
	counts := []GeneratedMediaCount{}
	if err := base().Select("kind, COUNT(*) AS count").Group("kind").Scan(&counts).Error; err != nil {
		return nil, nil, nil, err
	}
	dayExpression := "strftime('%Y-%m-%d', created_at, '+8 hours')"
	if r.db.Dialector.Name() == "postgres" {
		dayExpression = "to_char(created_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD')"
	}
	days := []GeneratedMediaDay{}
	if err := base().Select(dayExpression + " AS day, kind, COUNT(*) AS count").Group(dayExpression + ", kind").Order("day ASC, kind ASC").Scan(&days).Error; err != nil {
		return nil, nil, nil, err
	}
	models := []GeneratedMediaModel{}
	if err := base().Select("kind, model, COUNT(*) AS count").Group("kind, model").Order("count DESC, kind ASC, model ASC").Scan(&models).Error; err != nil {
		return nil, nil, nil, err
	}
	return counts, days, models, nil
}

func (r *Repository) GeneratedMediaTrackingStart() (time.Time, error) {
	var row struct{ AppliedAt time.Time }
	if err := r.db.Table("schema_migrations").Select("applied_at").Where("version = ?", 9).Take(&row).Error; err != nil {
		return time.Time{}, err
	}
	return row.AppliedAt, nil
}

func (r *Repository) GenerationTaskCount(from, to time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.Task{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Where("LOWER(type) LIKE ? OR LOWER(type) LIKE ?", "%image%", "%video%").
		Count(&count).Error
	return count, err
}

func (r *Repository) AnalyticsActiveUsers(from, to time.Time) (int64, error) {
	// Activity rows are UTC-day summaries, as in the existing admin overview.
	var count int64
	activityFrom := from.UTC().Truncate(24 * time.Hour)
	activityTo := to.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	err := r.db.Raw(`SELECT COUNT(*) FROM (
		SELECT user_id FROM user_daily_activities WHERE day >= ? AND day < ? AND (task_count > 0 OR agent_message_count > 0 OR canvas_active = ? OR asset_count > 0 OR resource_count > 0)
		UNION SELECT user_id FROM tasks WHERE created_at >= ? AND created_at < ?
		UNION SELECT user_id FROM api_call_logs WHERE created_at >= ? AND created_at < ?
	) AS active_users`, activityFrom, activityTo, true, from, to, from, to).Scan(&count).Error
	return count, err
}
