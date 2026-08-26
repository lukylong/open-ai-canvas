package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type CreateDistributionPublicationRequest struct {
	AssetVersionID string         `json:"assetVersionId"`
	Target         string         `json:"target"`
	Metadata       map[string]any `json:"metadata"`
}

const MaxDistributionBatchAssets = 1000

type CreateDistributionPublicationsRequest struct {
	AssetIDs []string       `json:"assetIds"`
	Target   string         `json:"target"`
	Metadata map[string]any `json:"metadata"`
}

type DistributionPublicationBatchItem struct {
	AssetID     string                         `json:"assetId"`
	Publication *model.DistributionPublication `json:"publication,omitempty"`
	Error       string                         `json:"error,omitempty"`
}

type DistributionPublicationBatchResult struct {
	RequestedCount int                                `json:"requestedCount"`
	AcceptedCount  int                                `json:"acceptedCount"`
	FailedCount    int                                `json:"failedCount"`
	Items          []DistributionPublicationBatchItem `json:"items"`
}

type distributionEvent struct {
	EventID   string                 `json:"event_id"`
	Source    string                 `json:"source"`
	Resources []distributionResource `json:"resources"`
}

type distributionResource struct {
	Action          string         `json:"action"`
	ExternalID      string         `json:"external_id"`
	Version         int64          `json:"version"`
	Type            int            `json:"type"`
	Title           string         `json:"title"`
	FileURL         string         `json:"file_url"`
	FilePath        string         `json:"file_path,omitempty"`
	SourceUpdatedAt string         `json:"source_updated_at"`
	Metadata        map[string]any `json:"metadata"`
}

func (s *Service) CreateDistributionPublication(user *model.User, assetID string, req CreateDistributionPublicationRequest) (*model.DistributionPublication, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	if !distributionConfigured() {
		return nil, BadAuthRequest("素材分发服务尚未配置")
	}
	asset, err := s.repo.AssetForUser(user.ID, strings.TrimSpace(assetID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("素材不存在")
	}
	if err != nil {
		return nil, err
	}
	if asset.Status == model.AssetVersionStatusArchived {
		return nil, BadAuthRequest("已归档素材不能分发")
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		target = "asset-distribution"
	}
	if target != "asset-distribution" {
		return nil, BadAuthRequest("不支持的分发目标")
	}
	versionID, resource, err := s.distributionAssetResource(user.ID, asset, strings.TrimSpace(req.AssetVersionID))
	if err != nil {
		return nil, err
	}
	if resource.Status != model.ResourceStatusReady {
		return nil, BadAuthRequest("素材原始文件尚未就绪")
	}
	fileURL, err := distributionResourceURL(resource)
	if err != nil {
		return nil, BadAuthRequest("素材原始文件缺少可分发地址")
	}

	metadata := map[string]any{"canvasAssetId": asset.ID, "canvasAssetVersionId": versionID, "resourceId": resource.ID, "provider": resource.Provider, "bucket": resource.Bucket, "mimeType": resource.MimeType, "width": resource.Width, "height": resource.Height, "durationMs": resource.DurationMs}
	for key, value := range distributionPayloadLineage(asset.PayloadJSON, asset.Title) {
		metadata[key] = value
	}
	for key, value := range req.Metadata {
		metadata[key] = value
	}
	item := distributionResource{Action: "upsert", ExternalID: asset.ID, Version: maxInt64(1, asset.UpdatedAt.UnixMilli()), Type: distributionAssetType(resource.Kind), Title: asset.Title, FileURL: fileURL, FilePath: resource.ObjectKey, SourceUpdatedAt: asset.UpdatedAt.UTC().Format(time.RFC3339Nano), Metadata: metadata}
	source := distributionSource()
	fingerprint, _ := json.Marshal(struct {
		Source         string `json:"source"`
		ExternalID     string `json:"external_id"`
		AssetVersionID string `json:"asset_version_id"`
		ResourceID     string `json:"resource_id"`
		Version        int64  `json:"version"`
	}{Source: source, ExternalID: asset.ID, AssetVersionID: versionID, ResourceID: resource.ID, Version: item.Version})
	digest := sha256.Sum256(fingerprint)
	event := distributionEvent{EventID: "canvas-assets-" + hex.EncodeToString(digest[:16]), Source: source, Resources: []distributionResource{item}}
	if existing, err := s.repo.DistributionPublicationByIdempotencyKey(user.ID, event.EventID); err == nil {
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	payload, _ := json.Marshal(event)
	now := time.Now()
	publication := model.DistributionPublication{ID: newID(), UserID: user.ID, AssetID: asset.ID, AssetVersionID: versionID, Target: target, Status: model.DistributionPublicationPending, PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now}
	outbox := model.DistributionOutbox{ID: newID(), PublicationID: publication.ID, EventType: "asset.upsert", IdempotencyKey: event.EventID, PayloadJSON: string(payload), Status: model.DistributionOutboxPending, NextAttemptAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateDistributionPublication(&publication, &outbox); err != nil {
		return nil, err
	}
	return &publication, nil
}

func distributionResourceURL(resource *model.Resource) (string, error) {
	if resource == nil {
		return "", errors.New("素材资源为空")
	}
	if publicURL := strings.TrimSpace(resource.PublicURL); publicURL != "" {
		return publicURL, nil
	}
	if strings.ToLower(strings.TrimSpace(resource.Provider)) != tencentCOSProvider {
		return "", errors.New("素材资源缺少公开地址")
	}
	baseURL, err := cosBucketBaseURL(ossSettingValue{
		Provider: tencentCOSProvider,
		Endpoint: resource.Endpoint,
		Bucket:   resource.Bucket,
	})
	if err != nil {
		return "", err
	}
	objectKey := strings.TrimLeft(strings.TrimSpace(resource.ObjectKey), "/")
	if objectKey == "" {
		return "", errors.New("素材资源对象路径为空")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/" + objectKey
	return baseURL.String(), nil
}

func (s *Service) CreateDistributionPublications(user *model.User, req CreateDistributionPublicationsRequest) (*DistributionPublicationBatchResult, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	if !distributionConfigured() {
		return nil, BadAuthRequest("素材分发服务尚未配置")
	}
	assetIDs := uniqueDistributionAssetIDs(req.AssetIDs)
	if len(assetIDs) == 0 {
		return nil, BadAuthRequest("请选择要分发的素材")
	}
	if len(assetIDs) > MaxDistributionBatchAssets {
		return nil, BadAuthRequest(fmt.Sprintf("单次最多分发 %d 个素材", MaxDistributionBatchAssets))
	}
	result := &DistributionPublicationBatchResult{
		RequestedCount: len(assetIDs),
		Items:          make([]DistributionPublicationBatchItem, 0, len(assetIDs)),
	}
	for _, assetID := range assetIDs {
		publication, err := s.CreateDistributionPublication(user, assetID, CreateDistributionPublicationRequest{Target: req.Target, Metadata: req.Metadata})
		item := DistributionPublicationBatchItem{AssetID: assetID, Publication: publication}
		if err != nil {
			item.Error = err.Error()
			result.FailedCount++
		} else {
			result.AcceptedCount++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func uniqueDistributionAssetIDs(values []string) []string {
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

func (s *Service) distributionAssetResource(userID string, asset *model.Asset, requestedVersionID string) (string, *model.Resource, error) {
	versions, representations, err := s.repo.AssetResourceRecords(asset.ID)
	if err != nil {
		return "", nil, err
	}
	versionID := requestedVersionID
	if versionID == "" {
		versionID = asset.PrimaryVersionID
	}
	versionFound := versionID == ""
	for _, version := range versions {
		if version.ID == versionID {
			versionFound = true
			break
		}
	}
	if requestedVersionID != "" && !versionFound {
		return "", nil, BadAuthRequest("素材版本不存在")
	}
	if versionFound && versionID != "" {
		for index := range representations {
			representation := &representations[index]
			if representation.AssetVersionID != versionID || representation.Role != "original" || representation.ResourceID == "" {
				continue
			}
			resource, resourceErr := s.repo.ResourceForUser(userID, representation.ResourceID)
			if resourceErr == nil {
				return versionID, resource, nil
			}
		}
	}

	resourceID := distributionPayloadResourceID(asset.PayloadJSON)
	if resourceID == "" {
		if versionID == "" || !versionFound {
			return "", nil, BadAuthRequest("素材版本不存在")
		}
		return "", nil, BadAuthRequest("素材版本缺少可分发的原始文件")
	}
	resource, err := s.repo.ResourceForUser(userID, resourceID)
	if err != nil {
		return "", nil, BadAuthRequest("素材原始文件不存在")
	}
	if versionID == "" || !versionFound {
		digest := sha256.Sum256([]byte(asset.ID))
		versionID = "payload-" + hex.EncodeToString(digest[:12])
	}
	return versionID, resource, nil
}

func distributionPayloadResourceID(payloadJSON string) string {
	var payload struct {
		Data struct {
			StorageKey string `json:"storageKey"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(payloadJSON), &payload) != nil {
		return ""
	}
	const prefix = "resource:"
	storageKey := strings.TrimSpace(payload.Data.StorageKey)
	if !strings.HasPrefix(storageKey, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(storageKey, prefix))
}

func distributionPayloadLineage(payloadJSON string, title string) map[string]any {
	var payload struct {
		Metadata map[string]any `json:"metadata"`
	}
	if json.Unmarshal([]byte(payloadJSON), &payload) != nil || payload.Metadata == nil {
		return nil
	}
	batchID := firstDistributionMetadataString(payload.Metadata, "batch_id", "batchId")
	taskID := firstDistributionMetadataString(payload.Metadata, "generation_task_id", "generationTaskId", "taskId")
	seriesID := batchID
	seriesType := "batch"
	if seriesID == "" {
		seriesID = taskID
		seriesType = "task"
	}
	if seriesID == "" {
		return nil
	}
	result := map[string]any{"series_id": seriesID, "series_type": seriesType, "series_label": title}
	if batchID != "" {
		result["batch_id"] = batchID
	}
	if taskID != "" {
		result["generation_task_id"] = taskID
	}
	for _, keys := range [][]string{{"batch_index", "batchIndex"}, {"batch_start_ordinal", "batchStartOrdinal"}} {
		for _, key := range keys {
			if value, exists := payload.Metadata[key]; exists {
				result[keys[0]] = value
				break
			}
		}
	}
	return result
}

func firstDistributionMetadataString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) DistributionPublications(user *model.User, limit int) ([]model.DistributionPublication, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	return s.repo.DistributionPublications(user.ID, limit)
}

func (s *Service) RetryDistributionPublication(user *model.User, id string) (*model.DistributionPublication, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	item, err := s.repo.RetryDistributionPublication(user.ID, id, time.Now())
	if errors.Is(err, repository.ErrDistributionOutboxUnavailable) {
		return nil, BadAuthRequest("仅失败的分发记录可以重试")
	}
	return item, err
}

func (s *Service) CancelDistributionPublication(user *model.User, id string) (*model.DistributionPublication, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	item, err := s.repo.CancelDistributionPublication(user.ID, id, time.Now())
	if errors.Is(err, repository.ErrDistributionOutboxUnavailable) {
		return nil, BadAuthRequest("仅待分发记录可以取消")
	}
	return item, err
}

func (s *Service) StartDistributionWorker() {
	if !distributionConfigured() {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_ = s.DispatchDistributionOnce(context.Background())
		}
	}()
}

func (s *Service) DispatchDistributionOnce(ctx context.Context) error {
	item, err := s.repo.ClaimDistributionOutbox(time.Now())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !distributionConfigured() {
		return s.repo.FailDistributionOutbox(item.ID, item.PublicationID, "分发服务尚未配置", time.Now().Add(time.Minute), true)
	}
	statusCode, responseBody, err := postDistribution(ctx, []byte(item.PayloadJSON))
	if err == nil && statusCode >= 200 && statusCode < 300 && distributionResponseAccepted(responseBody) {
		return s.repo.CompleteDistributionOutbox(item.ID, item.PublicationID, item.IdempotencyKey, time.Now())
	}
	if err == nil && statusCode >= 200 && statusCode < 300 {
		err = errors.New("分发服务未确认资源写入")
	}
	message := "分发请求失败"
	if err != nil {
		message = err.Error()
	} else {
		message = fmt.Sprintf("分发服务返回 HTTP %d: %s", statusCode, truncateRunes(strings.TrimSpace(string(responseBody)), 240))
	}
	terminal := item.Attempts >= 5 || statusCode >= 400 && statusCode < 500 && statusCode != http.StatusTooManyRequests
	delay := time.Duration(1<<minDistributionInt(item.Attempts, 6)) * time.Second
	return s.repo.FailDistributionOutbox(item.ID, item.PublicationID, message, time.Now().Add(delay), terminal)
}

func postDistribution(ctx context.Context, body []byte) (int, []byte, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomToken()[:32]
	canonical := timestamp + "\n" + nonce + "\n" + string(body)
	mac := hmac.New(sha256.New, []byte(os.Getenv("CANVAS_DISTRIBUTION_SECRET")))
	_, _ = mac.Write([]byte(canonical))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_URL")), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Asset-Sync-Key", strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_KEY_ID")))
	req.Header.Set("X-Asset-Sync-Timestamp", timestamp)
	req.Header.Set("X-Asset-Sync-Nonce", nonce)
	req.Header.Set("X-Asset-Sync-Signature", hex.EncodeToString(mac.Sum(nil)))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, responseBody, readErr
}

func distributionConfigured() bool {
	return strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_URL")) != "" && strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_KEY_ID")) != "" && strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_SECRET")) != ""
}

func distributionSource() string {
	if source := strings.TrimSpace(os.Getenv("CANVAS_DISTRIBUTION_SOURCE")); source != "" {
		return source
	}
	return "zq-media-studio"
}

func distributionResponseAccepted(body []byte) bool {
	var response struct {
		Code int `json:"code"`
		Data struct {
			Error int `json:"error"`
		} `json:"data"`
	}
	return json.Unmarshal(body, &response) == nil && response.Code == 1 && response.Data.Error == 0
}

func distributionAssetType(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return 2
	case "audio", "audio_reference":
		return 3
	default:
		return 1
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func minDistributionInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
