package service

import (
	"strings"
	"time"

	"infinite-canvas/backend/internal/repository"
)

type GeneratedMediaAnalytics struct {
	From             string                           `json:"from"`
	To               string                           `json:"to"`
	Timezone         string                           `json:"timezone"`
	GeneratedAt      time.Time                        `json:"generatedAt"`
	TrackingStarted  time.Time                        `json:"trackingStartedAt"`
	CoverageComplete bool                             `json:"coverageComplete"`
	ActiveUsers      int64                            `json:"activeUsers"`
	ActiveUsersBasis string                           `json:"activeUsersBasis"`
	GenerationTasks  int64                            `json:"generationTasks"`
	ImageOutputs     int64                            `json:"imageOutputs"`
	VideoOutputs     int64                            `json:"videoOutputs"`
	Trend            []repository.GeneratedMediaDay   `json:"trend"`
	Models           []repository.GeneratedMediaModel `json:"models"`
}

func (s *Service) GlobalGeneratedMediaAnalytics(fromText, toText string) (*GeneratedMediaAnalytics, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	localNow := time.Now().In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	if fromText == "" && toText == "" {
		fromText = today.AddDate(0, 0, -6).Format("2006-01-02")
		toText = today.Format("2006-01-02")
	}
	if strings.TrimSpace(fromText) != fromText || strings.TrimSpace(toText) != toText {
		return nil, NewAppError(400, "日期格式无效")
	}
	from, fromErr := time.ParseInLocation("2006-01-02", fromText, location)
	toDay, toErr := time.ParseInLocation("2006-01-02", toText, location)
	if fromErr != nil || toErr != nil || from.Format("2006-01-02") != fromText || toDay.Format("2006-01-02") != toText || toDay.Before(from) || toDay.Sub(from) > 365*24*time.Hour {
		return nil, NewAppError(400, "日期区间无效，最多366天")
	}
	end := toDay.AddDate(0, 0, 1)
	startUTC, endUTC := from.UTC(), end.UTC()
	counts, trend, models, err := s.repo.GeneratedMediaStats(startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	activeUsers, err := s.repo.AnalyticsActiveUsers(startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.GenerationTaskCount(startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	trackingStarted, err := s.repo.GeneratedMediaTrackingStart()
	if err != nil {
		return nil, err
	}
	result := &GeneratedMediaAnalytics{
		From: fromText, To: toText, Timezone: location.String(), GeneratedAt: time.Now().UTC(), TrackingStarted: trackingStarted,
		CoverageComplete: !startUTC.Before(trackingStarted), ActiveUsers: activeUsers, ActiveUsersBasis: "utc_day_overlap", GenerationTasks: tasks,
		Trend: trend, Models: models,
	}
	for _, item := range counts {
		switch item.Kind {
		case "image":
			result.ImageOutputs = item.Count
		case "video":
			result.VideoOutputs = item.Count
		}
	}
	return result, nil
}
