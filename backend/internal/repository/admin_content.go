package repository

import (
	"strings"

	"infinite-canvas/backend/internal/model"
)

type AdminGeneratedContentFilter struct {
	UserID       string
	Keyword      string
	Status       string
	Kind         string
	SourceSystem string
}

func (r *Repository) AdminGeneratedTasks(filter AdminGeneratedContentFilter, limit int, offset int) ([]model.Task, int64, error) {
	query := r.db.Model(&model.Task{})
	if userID := strings.TrimSpace(filter.UserID); userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if kind := strings.ToLower(strings.TrimSpace(filter.Kind)); kind != "" {
		query = query.Where("LOWER(type) = ? OR LOWER(type) LIKE ? OR LOWER(operation) = ?", kind, kind+"%", kind)
	}
	if keyword := strings.ToLower(strings.TrimSpace(filter.Keyword)); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(`
			LOWER(id) LIKE ? OR LOWER(prompt) LIKE ? OR LOWER(type) LIKE ? OR
			LOWER(model) LIKE ? OR LOWER(provider) LIKE ? OR user_id IN (
				SELECT id FROM users WHERE LOWER(username) LIKE ? OR LOWER(display_name) LIKE ?
			)
		`, like, like, like, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tasks []model.Task
	err := query.Select(
		"id", "user_id", "session_id", "project_id", "type", "status", "stage", "progress", "prompt",
		"operation", "provider", "model", "provider_request_id", "error", "attempts", "started_at", "completed_at", "created_at", "updated_at",
	).Order("created_at desc").Limit(limit).Offset(offset).Find(&tasks).Error
	return tasks, total, err
}

func (r *Repository) AdminGeneratedResources(filter AdminGeneratedContentFilter, limit int, offset int) ([]model.Resource, int64, error) {
	query := r.db.Model(&model.Resource{})
	if userID := strings.TrimSpace(filter.UserID); userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if kind := strings.ToLower(strings.TrimSpace(filter.Kind)); kind != "" {
		query = query.Where("LOWER(kind) = ?", kind)
	}
	if sourceSystem := strings.TrimSpace(filter.SourceSystem); sourceSystem != "" {
		if sourceSystem == "canvas" {
			query = query.Where("source_system = '' OR source_system = ?", sourceSystem)
		} else {
			query = query.Where("source_system = ?", sourceSystem)
		}
	}
	if keyword := strings.ToLower(strings.TrimSpace(filter.Keyword)); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(`
			LOWER(id) LIKE ? OR LOWER(object_key) LIKE ? OR LOWER(public_url) LIKE ? OR
			LOWER(kind) LIKE ? OR LOWER(source_system) LIKE ? OR user_id IN (
				SELECT id FROM users WHERE LOWER(username) LIKE ? OR LOWER(display_name) LIKE ?
			)
		`, like, like, like, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var resources []model.Resource
	err := query.Order("created_at desc").Limit(limit).Offset(offset).Find(&resources).Error
	return resources, total, err
}

func (r *Repository) UsersByIDs(ids []string) ([]model.User, error) {
	if len(ids) == 0 {
		return []model.User{}, nil
	}
	var users []model.User
	return users, r.db.Where("id IN ?", ids).Find(&users).Error
}
