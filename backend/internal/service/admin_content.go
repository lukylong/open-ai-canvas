package service

import (
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AdminGeneratedContentQuery struct {
	UserID       string
	Keyword      string
	Status       string
	Kind         string
	SourceSystem string
	Page         int
	Limit        int
}

type AdminContentUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type AdminGeneratedTask struct {
	model.Task
	User AdminContentUser `json:"user"`
}

type AdminGeneratedTaskPage struct {
	Tasks []AdminGeneratedTask `json:"tasks"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Limit int                  `json:"limit"`
}

type AdminGeneratedResource struct {
	model.Resource
	User       AdminContentUser `json:"user"`
	PreviewURL string           `json:"previewUrl"`
}

type AdminGeneratedResourcePage struct {
	Resources []AdminGeneratedResource `json:"resources"`
	Total     int64                    `json:"total"`
	Page      int                      `json:"page"`
	Limit     int                      `json:"limit"`
}

func (s *Service) AdminGeneratedTasks(actor *model.User, query AdminGeneratedContentQuery) (*AdminGeneratedTaskPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := s.validateAdminContentUser(query.UserID); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	tasks, total, err := s.repo.AdminGeneratedTasks(adminContentRepositoryFilter(query), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	users, err := s.adminContentUsers(taskUserIDs(tasks))
	if err != nil {
		return nil, err
	}
	items := make([]AdminGeneratedTask, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, AdminGeneratedTask{Task: task, User: users[task.UserID]})
	}
	return &AdminGeneratedTaskPage{Tasks: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminGeneratedTask(actor *model.User, taskID string) (*AdminGeneratedTask, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	task, err := s.repo.Task(strings.TrimSpace(taskID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFound("生成任务不存在")
		}
		return nil, err
	}
	user, err := s.repo.User(task.UserID)
	if err != nil {
		return nil, err
	}
	return &AdminGeneratedTask{Task: *task, User: AdminContentUser{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}}, nil
}

func (s *Service) AdminGeneratedResources(actor *model.User, query AdminGeneratedContentQuery) (*AdminGeneratedResourcePage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := s.validateAdminContentUser(query.UserID); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	resources, total, err := s.repo.AdminGeneratedResources(adminContentRepositoryFilter(query), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	users, err := s.adminContentUsers(resourceUserIDs(resources))
	if err != nil {
		return nil, err
	}
	items := make([]AdminGeneratedResource, 0, len(resources))
	for _, resource := range resources {
		items = append(items, AdminGeneratedResource{
			Resource:   resource,
			User:       users[resource.UserID],
			PreviewURL: "/api/admin/generated-content/resources/" + resource.ID + "/file",
		})
	}
	return &AdminGeneratedResourcePage{Resources: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) PrepareAdminGeneratedResourceDelivery(actor *model.User, resourceID string, rangeHeader string) (*ResourceDelivery, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	resource, err := s.repo.Resource(strings.TrimSpace(resourceID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFound("资源不存在")
		}
		return nil, err
	}
	delivery, err := s.prepareResourceDelivery(resource.UserID, resource, ResourceDeliveryOptions{})
	if err != nil || delivery.RedirectURL != "" {
		return delivery, err
	}
	stream, err := s.openResourceRange(resource.UserID, resource, rangeHeader)
	if err != nil {
		return nil, err
	}
	delivery.Stream = stream
	return delivery, nil
}

func (s *Service) validateAdminContentUser(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	_, err := s.repo.User(strings.TrimSpace(userID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("用户不存在")
	}
	return err
}

func (s *Service) adminContentUsers(ids []string) (map[string]AdminContentUser, error) {
	users, err := s.repo.UsersByIDs(adminContentUniqueStrings(ids))
	if err != nil {
		return nil, err
	}
	result := make(map[string]AdminContentUser, len(users))
	for _, user := range users {
		result[user.ID] = AdminContentUser{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}
	}
	return result, nil
}

func adminContentRepositoryFilter(query AdminGeneratedContentQuery) repository.AdminGeneratedContentFilter {
	return repository.AdminGeneratedContentFilter{
		UserID: query.UserID, Keyword: query.Keyword, Status: query.Status,
		Kind: query.Kind, SourceSystem: query.SourceSystem,
	}
}

func taskUserIDs(tasks []model.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.UserID)
	}
	return ids
}

func resourceUserIDs(resources []model.Resource) []string {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		ids = append(ids, resource.UserID)
	}
	return ids
}

func adminContentUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
