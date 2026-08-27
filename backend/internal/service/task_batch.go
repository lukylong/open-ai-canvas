package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type CreateTaskBatchRequest struct {
	Count          int               `json:"count"`
	IdempotencyKey string            `json:"idempotencyKey"`
	Task           CreateTaskRequest `json:"task"`
}

type TaskBatchItemOutput struct {
	model.TaskBatchItem
	Task *TaskBatchTaskOutput `json:"task,omitempty"`
}

type TaskBatchTaskOutput struct {
	TaskSummary
	ResultJSON string `json:"resultJson,omitempty"`
}

type TaskBatchDetail struct {
	Batch model.TaskBatch       `json:"batch"`
	Items []TaskBatchItemOutput `json:"items"`
}

func (s *Service) CreateTaskBatch(userID string, req CreateTaskBatchRequest) (*TaskBatchDetail, error) {
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" || len(key) > 160 {
		return nil, BadAuthRequest("批量任务缺少有效的幂等键")
	}
	if existing, err := s.repo.TaskBatchByIdempotencyKey(userID, key); err == nil {
		return s.TaskBatchDetail(userID, existing.ID)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	if req.Count < 1 || req.Count > policy.Task.BatchMaxCount {
		return nil, BadAuthRequest(fmt.Sprintf("单次批量生成数量必须在 1 到 %d 之间", policy.Task.BatchMaxCount))
	}
	mode, err := s.validateTaskBatchTemplate(userID, &req.Task)
	if err != nil {
		return nil, err
	}
	requestData, err := json.Marshal(req.Task)
	if err != nil {
		return nil, fmt.Errorf("序列化批量任务模板失败：%w", err)
	}
	now := time.Now()
	batch := model.TaskBatch{
		ID: newID(), UserID: userID, ProjectID: strings.TrimSpace(req.Task.ProjectID), Mode: mode,
		Status: model.TaskBatchStatusQueued, RequestedCount: req.Count, WaitingCount: req.Count,
		IdempotencyKey: key, RequestJSON: string(requestData), CreatedAt: now, UpdatedAt: now,
	}
	items := make([]model.TaskBatchItem, req.Count)
	for index := range items {
		items[index] = model.TaskBatchItem{
			ID: newID(), BatchID: batch.ID, Index: index, Status: model.TaskBatchItemStatusWaiting,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	if err := s.repo.CreateTaskBatch(&batch, items); errors.Is(err, repository.ErrTaskBatchIdempotency) {
		existing, findErr := s.repo.TaskBatchByIdempotencyKey(userID, key)
		if findErr != nil {
			return nil, findErr
		}
		return s.TaskBatchDetail(userID, existing.ID)
	} else if err != nil {
		return nil, err
	}
	return s.TaskBatchDetail(userID, batch.ID)
}

func (s *Service) validateTaskBatchTemplate(userID string, req *CreateTaskRequest) (string, error) {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return "", BadAuthRequest("批量生成提示词不能为空")
	}
	if err := validateTaskType(strings.TrimSpace(req.Type)); err != nil {
		return "", err
	}
	normalizedInput, err := normalizeTaskInput(req.Input)
	if err != nil {
		return "", err
	}
	if !taskBatchTemplateUsesManagedRuntime(req, normalizedInput) {
		return "", BadAuthRequest("批量生成必须选择后台平台模型、系统渠道或 ComfyUI 工作流")
	}
	mode := strings.TrimSpace(fmt.Sprint(normalizedInput["mode"]))
	if mode != "image" && mode != "video" {
		return "", BadAuthRequest("持久化批次当前只支持图片或视频任务")
	}
	if mode == "image" && !strings.HasPrefix(req.Type, "canvas_image") {
		return "", BadAuthRequest("图片批次任务类型无效")
	}
	if mode == "video" && !strings.HasPrefix(req.Type, "canvas_video") {
		return "", BadAuthRequest("视频批次任务类型无效")
	}
	logicalModelID := strings.TrimSpace(req.LogicalModelID)
	if logicalModelID != "" {
		intent := ModelRequestIntentFromTaskInput(normalizedInput, req.Type, req.Operation)
		routed, routeErr := s.ResolveLogicalModel(logicalModelID, intent)
		if routeErr != nil {
			return "", routeErr
		}
		normalizedInput = applyRoutedProviderSelection(normalizedInput, routed)
	} else if !taskInputUsesWorkflowProvider(normalizedInput) {
		if err := s.validateSystemChannelModelSelection(normalizedInput); err != nil {
			return "", err
		}
	}
	if err := s.ValidateTaskCapability(normalizedInput); err != nil {
		return "", err
	}
	if containsInlineMediaDataURL(normalizedInput) {
		return "", BadAuthRequest("批量任务输入不能包含内嵌媒体，请先上传到资源存储")
	}
	if err := s.ensureTaskProjectActive(userID, req.ProjectID); err != nil {
		return "", err
	}
	// 只持久化经过校验的托管模板：逻辑模型已经移除供应链字段，系统渠道只保留
	// 固定占位密钥，ComfyUI Bridge 只保留服务器工作流引用。
	req.Input = normalizedInput
	return mode, nil
}

// 批次模板会在数据库中长期保存，所以只接受不携带用户明文密钥的后端托管运行时。
// 系统渠道使用固定占位密钥，ComfyUI Bridge 使用服务器本机配置；RunningHub 和
// 自定义渠道仍需先改造成服务端密钥引用，不能把真实密钥复制到整个批次模板。
func taskBatchTemplateUsesManagedRuntime(req *CreateTaskRequest, input map[string]any) bool {
	if strings.TrimSpace(req.LogicalModelID) != "" {
		return true
	}
	config, _ := input["config"].(map[string]any)
	interfaceType := strings.TrimSpace(fmt.Sprint(config["interfaceType"]))
	if isComfyBridgeInterface(interfaceType) {
		return true
	}
	if taskInputUsesWorkflowProvider(input) {
		return false
	}
	channelID := strings.TrimSpace(fmt.Sprint(config["channelId"]))
	baseURL := strings.TrimSpace(fmt.Sprint(config["baseUrl"]))
	apiKey := strings.TrimSpace(fmt.Sprint(config["apiKey"]))
	return (channelID != "" || systemChannelIDFromBaseURL(baseURL) != "") && strings.EqualFold(apiKey, "system")
}

func (s *Service) TaskBatches(userID string, limit int) ([]model.TaskBatch, error) {
	return s.repo.TaskBatchesForUser(userID, limit)
}

func (s *Service) TaskBatchDetail(userID string, id string) (*TaskBatchDetail, error) {
	if _, err := s.repo.TaskBatchForUser(userID, id); err != nil {
		return nil, err
	}
	if err := s.syncTaskBatch(id); err != nil {
		return nil, err
	}
	batch, err := s.repo.TaskBatchForUser(userID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.TaskBatchItems(batch.ID)
	if err != nil {
		return nil, err
	}
	taskIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.TaskID != "" {
			taskIDs = append(taskIDs, item.TaskID)
		}
	}
	tasks, err := s.repo.TasksByIDsForUser(userID, taskIDs)
	if err != nil {
		return nil, err
	}
	taskByID := make(map[string]model.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	output := make([]TaskBatchItemOutput, len(items))
	for index, item := range items {
		output[index].TaskBatchItem = item
		if task, ok := taskByID[item.TaskID]; ok {
			summary := taskSummaryForOutput(task)
			output[index].Task = &TaskBatchTaskOutput{TaskSummary: summary, ResultJSON: task.ResultJSON}
		}
	}
	return &TaskBatchDetail{Batch: *batch, Items: output}, nil
}

func (s *Service) PauseTaskBatch(userID string, id string) (*TaskBatchDetail, error) {
	if err := s.repo.PauseTaskBatchForUser(userID, id, ""); err != nil {
		return nil, err
	}
	return s.TaskBatchDetail(userID, id)
}

func (s *Service) ResumeTaskBatch(userID string, id string) (*TaskBatchDetail, error) {
	if err := s.repo.ResumeTaskBatchForUser(userID, id); err != nil {
		return nil, err
	}
	return s.TaskBatchDetail(userID, id)
}

func (s *Service) CancelWaitingTaskBatchItems(userID string, id string) (*TaskBatchDetail, error) {
	if err := s.repo.CancelWaitingTaskBatchItemsForUser(userID, id); err != nil {
		return nil, err
	}
	return s.TaskBatchDetail(userID, id)
}

func (s *Service) RetryFailedTaskBatchItems(userID string, id string) (*TaskBatchDetail, error) {
	if err := s.repo.RetryFailedTaskBatchItemsForUser(userID, id); err != nil {
		return nil, err
	}
	return s.TaskBatchDetail(userID, id)
}

func (s *Service) syncActiveTaskBatches() error {
	batches, err := s.repo.ActiveTaskBatches()
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if err := s.syncTaskBatch(batch.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) syncTaskBatch(batchID string) error {
	batch, err := s.repo.TaskBatch(batchID)
	if err != nil {
		return err
	}
	items, err := s.repo.TaskBatchItems(batchID)
	if err != nil {
		return err
	}
	taskIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.TaskID != "" {
			taskIDs = append(taskIDs, item.TaskID)
		}
	}
	tasks, err := s.repo.TasksByIDsForUser(batch.UserID, taskIDs)
	if err != nil {
		return err
	}
	taskByID := make(map[string]model.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	for index := range items {
		item := &items[index]
		if item.RetryRequested || item.TaskID == "" {
			continue
		}
		task, ok := taskByID[item.TaskID]
		if !ok {
			continue
		}
		status := taskBatchItemStatusForTask(task.Status)
		if status != item.Status || task.Error != item.Error {
			if err := s.repo.UpdateTaskBatchItemFromTask(item.ID, status, task.Error); err != nil {
				return err
			}
			item.Status = status
			item.Error = task.Error
		}
	}
	waiting, queued, running, succeeded, failed, cancelled := 0, 0, 0, 0, 0, 0
	for _, item := range items {
		switch item.Status {
		case model.TaskBatchItemStatusWaiting, model.TaskBatchItemStatusSubmitting:
			waiting++
		case model.TaskBatchItemStatusQueued:
			queued++
		case model.TaskBatchItemStatusRunning:
			running++
		case model.TaskBatchItemStatusSucceeded:
			succeeded++
		case model.TaskBatchItemStatusFailed:
			failed++
		case model.TaskBatchItemStatusCancelled:
			cancelled++
		}
	}
	status := batch.Status
	var completedAt any
	terminalCount := succeeded + failed + cancelled
	if status != model.TaskBatchStatusPaused && status != model.TaskBatchStatusCancelled {
		switch {
		case terminalCount == batch.RequestedCount && failed == 0 && cancelled == 0:
			status = model.TaskBatchStatusSucceeded
			now := time.Now()
			completedAt = &now
		case terminalCount == batch.RequestedCount:
			status = model.TaskBatchStatusCompletedWithErrors
			now := time.Now()
			completedAt = &now
		case queued+running > 0:
			status = model.TaskBatchStatusRunning
		default:
			status = model.TaskBatchStatusQueued
		}
	}
	values := map[string]any{
		"waiting_count": waiting, "queued_count": queued, "running_count": running,
		"succeeded_count": succeeded, "failed_count": failed, "cancelled_count": cancelled, "status": status,
	}
	if completedAt != nil {
		values["completed_at"] = completedAt
	}
	return s.repo.UpdateTaskBatchSnapshot(batchID, values)
}

func taskBatchItemStatusForTask(status model.TaskStatus) model.TaskBatchItemStatus {
	switch status {
	case model.TaskStatusQueued:
		return model.TaskBatchItemStatusQueued
	case model.TaskStatusRunning:
		return model.TaskBatchItemStatusRunning
	case model.TaskStatusSucceeded:
		return model.TaskBatchItemStatusSucceeded
	case model.TaskStatusCancelled:
		return model.TaskBatchItemStatusCancelled
	default:
		return model.TaskBatchItemStatusFailed
	}
}

func (s *Service) promoteTaskBatchItems() error {
	if err := s.syncActiveTaskBatches(); err != nil {
		return err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return err
	}
	for promoted := 0; promoted < policy.Task.ActiveTaskLimit; promoted++ {
		batch, item, err := s.repo.ClaimNextTaskBatchItem("batch:"+s.workerID, 45*time.Second)
		if err != nil || item == nil {
			return err
		}
		if item.TaskID != "" && item.RetryRequested {
			task, retryErr := s.RetryTask(batch.UserID, item.TaskID)
			if retryErr == nil {
				if err := s.repo.LinkTaskBatchItem(item.ID, task.ID, model.TaskBatchItemStatusQueued); err != nil {
					return err
				}
				continue
			}
			if errors.Is(retryErr, repository.ErrActiveTaskLimit) {
				_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusWaiting, "")
				break
			}
			_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusFailed, retryErr.Error())
			continue
		}
		if existing, findErr := s.repo.TaskByBatchItemID(item.ID); findErr == nil {
			if err := s.repo.LinkTaskBatchItem(item.ID, existing.ID, taskBatchItemStatusForTask(existing.Status)); err != nil {
				return err
			}
			continue
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		var req CreateTaskRequest
		if err := json.Unmarshal([]byte(batch.RequestJSON), &req); err != nil {
			_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusFailed, "批量任务模板无效")
			continue
		}
		metadata, _ := req.Input["metadata"].(map[string]any)
		metadata = cloneAnyMap(metadata)
		metadata["batchId"] = batch.ID
		metadata["batchIndex"] = item.Index
		metadata["batchCount"] = batch.RequestedCount
		req.Input["metadata"] = metadata
		task, createErr := s.createTask(batch.UserID, req, &taskBatchLink{BatchID: batch.ID, ItemID: item.ID, Index: item.Index})
		if createErr == nil {
			if err := s.repo.LinkTaskBatchItem(item.ID, task.ID, model.TaskBatchItemStatusQueued); err != nil {
				return err
			}
			continue
		}
		if errors.Is(createErr, repository.ErrActiveTaskLimit) {
			_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusWaiting, "")
			break
		}
		if errors.Is(createErr, repository.ErrInsufficientCredits) {
			_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusWaiting, "")
			_ = s.repo.PauseTaskBatchForUser(batch.UserID, batch.ID, createErr.Error())
			break
		}
		_ = s.repo.ReleaseTaskBatchItem(item.ID, model.TaskBatchItemStatusFailed, createErr.Error())
	}
	return s.syncActiveTaskBatches()
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+3)
	for key, value := range source {
		result[key] = value
	}
	return result
}
