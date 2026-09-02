package service

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const (
	sharedSingleMaxBytes int64 = 50 << 20
	sharedBatchMaxFiles        = 1000
	sharedBatchMaxBytes  int64 = 5 << 30
	sharedZIPMaxBytes    int64 = 2 << 30
	sharedZIPMaxEntries        = 5000
	sharedZIPMaxRatio    int64 = 100
	sharedUploadURLTTL         = 15 * time.Minute
	sharedUploadLease          = 2 * time.Minute
)

type SharedUploadPolicy struct {
	AllowedExtensions      []string `json:"allowedExtensions"`
	AllowedMimeTypes       []string `json:"allowedMimeTypes"`
	SingleMaxBytes         int64    `json:"singleMaxBytes"`
	BatchMaxFiles          int      `json:"batchMaxFiles"`
	BatchMaxBytes          int64    `json:"batchMaxBytes"`
	ZIPMaxBytes            int64    `json:"zipMaxBytes"`
	ZIPExtractedMaxFiles   int      `json:"zipExtractedMaxFiles"`
	ZIPExtractedMaxBytes   int64    `json:"zipExtractedMaxBytes"`
	ZIPMaxEntries          int      `json:"zipMaxEntries"`
	ZIPMaxCompressionRatio int64    `json:"zipMaxCompressionRatio"`
	UploadURLTTLSeconds    int      `json:"uploadUrlTtlSeconds"`
	DefaultConcurrency     int      `json:"defaultConcurrency"`
	MaxConcurrency         int      `json:"maxConcurrency"`
	Description            string   `json:"description"`
}

type SharedUploadManifestItem struct {
	ClientID string `json:"clientId"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type CreateSharedUploadBatchRequest struct {
	Mode             string                     `json:"mode"`
	SeriesID         string                     `json:"seriesId"`
	SeriesName       string                     `json:"seriesName"`
	Files            []SharedUploadManifestItem `json:"files"`
	ZIPEntryCount    int                        `json:"zipEntryCount"`
	ZIPDeclaredBytes int64                      `json:"zipDeclaredBytes"`
	ZIPEncrypted     bool                       `json:"zipEncrypted"`
}

type SharedUploadTarget struct {
	ItemID    string    `json:"itemId"`
	UploadURL string    `json:"uploadUrl"`
	Method    string    `json:"method"`
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"token"`
}

type SharedUploadBatchDetail struct {
	Batch   model.SharedAssetUploadBatch  `json:"batch"`
	Series  *model.SharedAssetSeries      `json:"series,omitempty"`
	Items   []model.SharedAssetUploadItem `json:"items"`
	Uploads []SharedUploadTarget          `json:"uploads,omitempty"`
}

func SharedLibraryUploadPolicy() SharedUploadPolicy {
	return SharedUploadPolicy{
		AllowedExtensions: []string{".jpg", ".jpeg", ".png", ".webp"},
		AllowedMimeTypes:  []string{"image/jpeg", "image/png", "image/webp"},
		SingleMaxBytes:    sharedSingleMaxBytes, BatchMaxFiles: sharedBatchMaxFiles, BatchMaxBytes: sharedBatchMaxBytes,
		ZIPMaxBytes: sharedZIPMaxBytes, ZIPExtractedMaxFiles: sharedBatchMaxFiles, ZIPExtractedMaxBytes: sharedBatchMaxBytes,
		ZIPMaxEntries: sharedZIPMaxEntries, ZIPMaxCompressionRatio: sharedZIPMaxRatio,
		UploadURLTTLSeconds: int(sharedUploadURLTTL.Seconds()), DefaultConcurrency: 4, MaxConcurrency: 6,
		Description: "支持 JPG、PNG、WebP｜单张最大50MB｜普通批量最多1000张、合计5GB｜ZIP最大2GB，解压后最多1000张/5GB。",
	}
}

func (s *Service) RequireSharedLibraryAccess(user *model.User) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	if err := s.RequireFeature(FeatureSharedLibrary); err != nil {
		return err
	}
	if user.Status != model.UserStatusActive {
		return Forbidden("该账号已被禁用")
	}
	if user.Role == model.UserRoleAdmin || user.SharedLibraryEnabled {
		return nil
	}
	return Forbidden("当前账号未开通共享素材库权限")
}

func (s *Service) UpdateUserSharedLibraryAccess(actor *model.User, userID string, enabled bool) (*model.User, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	target, err := s.repo.User(userID)
	if err != nil {
		return nil, err
	}
	if target.SharedLibraryEnabled == enabled {
		return target, nil
	}
	event := &model.AdminAuditEvent{ID: newID(), ActorUserID: actor.ID, Action: "shared_library.access.update", TargetType: "user", TargetID: userID,
		Summary: "更新共享素材库账号权限", MetadataJSON: sharedJSON(map[string]any{"before": target.SharedLibraryEnabled, "after": enabled}), CreatedAt: time.Now()}
	return s.repo.UpdateUserSharedLibraryAccess(userID, enabled, event)
}

func (s *Service) SharedAssetSeriesList(user *model.User) ([]model.SharedAssetSeries, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	return s.repo.SharedAssetSeriesList()
}

func (s *Service) CreateSharedAssetSeries(user *model.User, name string) (*model.SharedAssetSeries, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return nil, BadAuthRequest("系列名称必须为 1-80 个字符")
	}
	now := time.Now()
	row := &model.SharedAssetSeries{ID: newID(), Name: name, OwnerUserID: user.ID, Status: model.SharedAssetSeriesReady, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveSharedAssetSeries(row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) UpdateSharedAssetSeries(user *model.User, id string, name string) (*model.SharedAssetSeries, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	row, err := s.repo.SharedAssetSeries(id)
	if err != nil {
		return nil, err
	}
	if user.Role != model.UserRoleAdmin && row.OwnerUserID != user.ID {
		return nil, Forbidden("只能管理自己创建的共享系列")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return nil, BadAuthRequest("系列名称必须为 1-80 个字符")
	}
	row.Name, row.UpdatedAt = name, time.Now()
	if err := s.repo.SaveSharedAssetSeries(row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) DeleteSharedAssetSeries(user *model.User, id string) error {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return err
	}
	row, err := s.repo.SharedAssetSeries(id)
	if err != nil {
		return err
	}
	if user.Role != model.UserRoleAdmin && row.OwnerUserID != user.ID {
		return Forbidden("只能管理自己创建的共享系列")
	}
	row.Status, row.UpdatedAt = model.SharedAssetSeriesArchived, time.Now()
	return s.repo.SaveSharedAssetSeries(row)
}

func (s *Service) SharedAssets(user *model.User, seriesID string) ([]model.SharedAsset, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	if seriesID != "" {
		series, err := s.repo.SharedAssetSeries(seriesID)
		if err != nil || series.Status != model.SharedAssetSeriesReady {
			return nil, NotFound("共享素材系列不存在")
		}
	}
	return s.repo.SharedAssets(seriesID)
}

func (s *Service) CreateSharedUploadBatch(user *model.User, req CreateSharedUploadBatchRequest) (*SharedUploadBatchDetail, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != "files" && mode != "zip" {
		return nil, BadAuthRequest("上传模式必须为 files 或 zip")
	}
	if len(req.Files) == 0 || (mode == "zip" && len(req.Files) != 1) {
		return nil, BadAuthRequest("上传清单不能为空，ZIP 批次必须只包含一个压缩包")
	}
	if mode == "files" && len(req.Files) > sharedBatchMaxFiles {
		return nil, BadAuthRequest("普通批量最多上传 1000 张图片")
	}
	if mode == "zip" && (req.ZIPEncrypted || req.ZIPEntryCount > sharedZIPMaxEntries || req.ZIPDeclaredBytes > sharedBatchMaxBytes) {
		return nil, BadAuthRequest("ZIP 中央目录预检未通过")
	}
	var series *model.SharedAssetSeries
	if mode == "zip" {
		name := strings.TrimSpace(req.SeriesName)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(req.Files[0].FileName), filepath.Ext(req.Files[0].FileName))
		}
		if name == "" || len([]rune(name)) > 80 {
			return nil, BadAuthRequest("ZIP 系列名称必须为 1-80 个字符")
		}
		now := time.Now()
		series = &model.SharedAssetSeries{ID: newID(), Name: name, OwnerUserID: user.ID, Status: model.SharedAssetSeriesPreparing, CreatedAt: now, UpdatedAt: now}
		req.SeriesID = series.ID
	} else {
		owned, err := s.repo.SharedAssetSeries(req.SeriesID)
		if err != nil || owned.Status == model.SharedAssetSeriesArchived {
			return nil, BadAuthRequest("请选择有效的共享素材系列")
		}
		if user.Role != model.UserRoleAdmin && owned.OwnerUserID != user.ID {
			return nil, Forbidden("不能上传到其他用户的共享系列")
		}
	}

	total := int64(0)
	seen := map[string]bool{}
	now := time.Now()
	batch := &model.SharedAssetUploadBatch{ID: newID(), OwnerUserID: user.ID, SeriesID: req.SeriesID, Mode: mode, Status: model.SharedBatchPreparing, FileCount: len(req.Files), NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	items := make([]model.SharedAssetUploadItem, 0, len(req.Files))
	uploads := make([]SharedUploadTarget, 0, len(req.Files))
	for index, input := range req.Files {
		input.FileName = filepath.Base(strings.TrimSpace(input.FileName))
		input.ClientID = strings.TrimSpace(input.ClientID)
		if input.ClientID == "" {
			input.ClientID = fmt.Sprintf("file-%d", index+1)
		}
		if seen[input.ClientID] {
			return nil, BadAuthRequest("上传清单 clientId 重复")
		}
		seen[input.ClientID] = true
		if input.Size <= 0 || (mode == "files" && input.Size > sharedSingleMaxBytes) || (mode == "zip" && input.Size > sharedZIPMaxBytes) {
			return nil, BadAuthRequest("上传文件大小超过策略限制")
		}
		if mode == "files" && !allowedSharedImageName(input.FileName) {
			return nil, BadAuthRequest("仅支持 JPG、PNG、WebP 图片")
		}
		if mode == "zip" && !strings.EqualFold(filepath.Ext(input.FileName), ".zip") {
			return nil, BadAuthRequest("请选择 ZIP 压缩包")
		}
		if normalized := strings.ToLower(strings.TrimSpace(input.SHA256)); normalized != "" && (len(normalized) != 64 || !isHex(normalized)) {
			return nil, BadAuthRequest("SHA-256 校验值格式无效")
		}
		total += input.Size
		token := randomToken()
		expires := now.Add(sharedUploadURLTTL)
		item := model.SharedAssetUploadItem{ID: newID(), BatchID: batch.ID, ClientID: input.ClientID, FileName: input.FileName, DeclaredMime: input.MimeType,
			ExpectedSize: input.Size, ExpectedSHA256: strings.ToLower(input.SHA256), Status: model.SharedItemPending, UploadTokenHash: hashToken(token), UploadExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
		item.StagingPath = s.sharedStagingPath(batch.ID, item.ID, mode == "zip")
		items = append(items, item)
		uploads = append(uploads, SharedUploadTarget{ItemID: item.ID, UploadURL: "/api/shared-library/upload-batches/" + batch.ID + "/items/" + item.ID + "/content", Method: http.MethodPut, ExpiresAt: expires, Token: token})
	}
	limit := sharedBatchMaxBytes
	if mode == "zip" {
		limit = sharedZIPMaxBytes
	}
	if total > limit {
		return nil, BadAuthRequest("上传批次总大小超过策略限制")
	}
	batch.TotalBytes, batch.Status = total, model.SharedBatchUploading
	if err := s.repo.CreateSharedUploadBatch(batch, items, series); err != nil {
		return nil, err
	}
	return &SharedUploadBatchDetail{Batch: *batch, Series: series, Items: items, Uploads: uploads}, nil
}

func (s *Service) SharedUploadBatchDetail(user *model.User, id string) (*SharedUploadBatchDetail, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	batch, err := s.repo.SharedUploadBatchForUser(user.ID, id)
	if err != nil && user.Role == model.UserRoleAdmin {
		batch, err = s.repo.SharedUploadBatch(id)
	}
	if err != nil {
		return nil, err
	}
	items, err := s.repo.SharedUploadItems(batch.ID)
	if err != nil {
		return nil, err
	}
	series, _ := s.repo.SharedAssetSeries(batch.SeriesID)
	return &SharedUploadBatchDetail{Batch: *batch, Series: series, Items: items}, nil
}

func (s *Service) RenewSharedUploadBatch(user *model.User, id string) (*SharedUploadBatchDetail, error) {
	detail, err := s.SharedUploadBatchDetail(user, id)
	if err != nil {
		return nil, err
	}
	if detail.Batch.OwnerUserID != user.ID && user.Role != model.UserRoleAdmin {
		return nil, Forbidden("无权续签该上传批次")
	}
	if detail.Batch.Status != model.SharedBatchUploading && detail.Batch.Status != model.SharedBatchPreparing {
		return nil, BadAuthRequest("当前批次无需续签")
	}
	now := time.Now()
	for index := range detail.Items {
		item := &detail.Items[index]
		if item.Status == model.SharedItemReady {
			continue
		}
		token, expires := randomToken(), now.Add(sharedUploadURLTTL)
		item.UploadTokenHash, item.UploadExpiresAt, item.UpdatedAt = hashToken(token), &expires, now
		if err := s.repo.SaveSharedUploadItem(item); err != nil {
			return nil, err
		}
		detail.Uploads = append(detail.Uploads, SharedUploadTarget{ItemID: item.ID, UploadURL: "/api/shared-library/upload-batches/" + id + "/items/" + item.ID + "/content", Method: http.MethodPut, ExpiresAt: expires, Token: token})
	}
	return detail, nil
}

func (s *Service) UploadSharedItemContent(user *model.User, batchID, itemID, token string, body io.Reader) (*model.SharedAssetUploadItem, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	item, err := s.repo.SharedUploadItemForUser(user.ID, batchID, itemID)
	if err != nil {
		return nil, err
	}
	if item.Status == model.SharedItemReady {
		return item, nil
	}
	if item.UploadExpiresAt == nil || time.Now().After(*item.UploadExpiresAt) || hashToken(strings.TrimSpace(token)) != item.UploadTokenHash {
		return nil, Forbidden("上传地址已过期，请续签后重试")
	}
	if err := os.MkdirAll(filepath.Dir(item.StagingPath), 0o750); err != nil {
		return nil, err
	}
	tmp := item.StagingPath + ".part"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	item.Status, item.UpdatedAt = model.SharedItemUploading, time.Now()
	_ = s.repo.SaveSharedUploadItem(item)
	written, copyErr := io.Copy(file, io.LimitReader(body, item.ExpectedSize+1))
	closeErr := file.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && written != item.ExpectedSize {
		copyErr = fmt.Errorf("实际上传大小 %d 与声明大小 %d 不一致", written, item.ExpectedSize)
	}
	if copyErr != nil {
		_ = os.Remove(tmp)
		item.Status, item.Error, item.UpdatedAt = model.SharedItemFailed, copyErr.Error(), time.Now()
		_ = s.repo.SaveSharedUploadItem(item)
		return nil, copyErr
	}
	if err := os.Rename(tmp, item.StagingPath); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	item.Status, item.Error, item.UpdatedAt = model.SharedItemVerifying, "", time.Now()
	if err := s.repo.SaveSharedUploadItem(item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) CompleteSharedUploadItem(user *model.User, batchID, itemID string) (*SharedUploadBatchDetail, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	item, err := s.repo.SharedUploadItemForUser(user.ID, batchID, itemID)
	if err != nil {
		return nil, err
	}
	batch, err := s.repo.SharedUploadBatchForUser(user.ID, batchID)
	if err != nil {
		return nil, err
	}
	if item.Status == model.SharedItemReady || item.AssetID != "" {
		return s.SharedUploadBatchDetail(user, batchID)
	}
	if item.StagingPath == "" {
		return nil, BadAuthRequest("上传文件不存在")
	}
	if batch.Mode == "zip" {
		if _, _, err := verifyStagedFile(item, false); err != nil {
			return nil, err
		}
		batch.ArchivePath, batch.Status, batch.NextAttemptAt, batch.UpdatedAt = item.StagingPath, model.SharedBatchQueued, time.Now(), time.Now()
		item.Status, item.UpdatedAt = model.SharedItemReady, time.Now()
		if err := s.repo.SaveSharedUploadItem(item); err != nil {
			return nil, err
		}
		if err := s.repo.SaveSharedUploadBatch(batch); err != nil {
			return nil, err
		}
		return s.SharedUploadBatchDetail(user, batchID)
	}
	if err := s.importSharedStagedImage(batch, item); err != nil {
		return nil, err
	}
	items, _ := s.repo.SharedUploadItems(batch.ID)
	finished := true
	for _, row := range items {
		if row.Status != model.SharedItemReady && row.Status != model.SharedItemSkipped && row.Status != model.SharedItemFailed {
			finished = false
			break
		}
	}
	if finished {
		_ = s.completeSharedBatch(batch, "")
	}
	return s.SharedUploadBatchDetail(user, batchID)
}

func (s *Service) RetrySharedUploadItem(user *model.User, batchID, itemID string) (*SharedUploadBatchDetail, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	item, err := s.repo.SharedUploadItemForUser(user.ID, batchID, itemID)
	if err != nil {
		return nil, err
	}
	if item.Status == model.SharedItemReady {
		return s.SharedUploadBatchDetail(user, batchID)
	}
	item.Status, item.Error, item.UpdatedAt = model.SharedItemPending, "", time.Now()
	if err := s.repo.SaveSharedUploadItem(item); err != nil {
		return nil, err
	}
	return s.RenewSharedUploadBatch(user, batchID)
}

func (s *Service) CancelSharedUploadBatch(user *model.User, id string) (*SharedUploadBatchDetail, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	batch, err := s.repo.SharedUploadBatchForUser(user.ID, id)
	if err != nil {
		return nil, err
	}
	if batch.Status == model.SharedBatchCompleted || batch.Status == model.SharedBatchCompletedWithErrors {
		return nil, BadAuthRequest("已完成批次不能取消")
	}
	batch.Status, batch.UpdatedAt = model.SharedBatchCancelled, time.Now()
	if err := s.repo.SaveSharedUploadBatch(batch); err != nil {
		return nil, err
	}
	return s.SharedUploadBatchDetail(user, id)
}

func (s *Service) UpdateSharedAsset(user *model.User, id, title string) (*model.SharedAsset, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	asset, err := s.repo.SharedAsset(id)
	if err != nil {
		return nil, err
	}
	series, err := s.repo.SharedAssetSeries(asset.SeriesID)
	if err != nil {
		return nil, err
	}
	if user.Role != model.UserRoleAdmin && series.OwnerUserID != user.ID {
		return nil, Forbidden("只能管理自己创建的共享素材")
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 120 {
		return nil, BadAuthRequest("素材标题必须为 1-120 个字符")
	}
	asset.Title, asset.Version, asset.UpdatedAt = title, asset.Version+1, time.Now()
	if err := s.repo.SaveSharedAsset(asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Service) DeleteSharedAsset(user *model.User, id string) error {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return err
	}
	asset, err := s.repo.SharedAsset(id)
	if err != nil {
		return err
	}
	series, err := s.repo.SharedAssetSeries(asset.SeriesID)
	if err != nil {
		return err
	}
	if user.Role != model.UserRoleAdmin && series.OwnerUserID != user.ID {
		return Forbidden("只能管理自己创建的共享素材")
	}
	asset.Status, asset.Version, asset.UpdatedAt = model.SharedAssetArchived, asset.Version+1, time.Now()
	return s.repo.SaveSharedAsset(asset)
}

func (s *Service) DeleteProjectSharedAsset(user *model.User, projectID, sharedAssetID string) error {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return err
	}
	if _, err := s.repo.ProjectForUser(user.ID, projectID); err != nil {
		return err
	}
	return s.repo.DeleteProjectSharedAsset(projectID, sharedAssetID)
}

func (s *Service) PrepareSharedAssetDelivery(user *model.User, id string, thumbnail bool, rangeHeader string) (*ResourceDelivery, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	asset, err := s.repo.SharedAsset(id)
	if err != nil || asset.Status != model.SharedAssetReady {
		return nil, NotFound("共享素材不存在")
	}
	series, err := s.repo.SharedAssetSeries(asset.SeriesID)
	if err != nil || series.Status != model.SharedAssetSeriesReady {
		return nil, NotFound("共享素材系列不存在")
	}
	resourceID := asset.ResourceID
	if thumbnail && asset.ThumbnailResourceID != "" {
		resourceID = asset.ThumbnailResourceID
	}
	resource, err := s.repo.Resource(resourceID)
	if err != nil {
		return nil, err
	}
	return s.prepareResourceDelivery(resource.UserID, resource, ResourceDeliveryOptions{})
}

func (s *Service) OpenSharedAssetRange(user *model.User, id string, thumbnail bool, rangeHeader string) (*ResourceStream, error) {
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return nil, err
	}
	asset, err := s.repo.SharedAsset(id)
	if err != nil || asset.Status != model.SharedAssetReady {
		return nil, NotFound("共享素材不存在")
	}
	series, err := s.repo.SharedAssetSeries(asset.SeriesID)
	if err != nil || series.Status != model.SharedAssetSeriesReady {
		return nil, NotFound("共享素材系列不存在")
	}
	resourceID := asset.ResourceID
	if thumbnail && asset.ThumbnailResourceID != "" {
		resourceID = asset.ThumbnailResourceID
	}
	resource, err := s.repo.Resource(resourceID)
	if err != nil {
		return nil, err
	}
	return s.openResourceRange(resource.UserID, resource, rangeHeader)
}

func (s *Service) ValidateSharedAssetReferences(userID string, value any) error {
	var ids []string
	collectSharedAssetIDs(value, &ids)
	if len(ids) == 0 {
		return nil
	}
	user, err := s.repo.User(userID)
	if err != nil {
		return err
	}
	if err := s.RequireSharedLibraryAccess(user); err != nil {
		return err
	}
	for _, id := range uniqueNonEmpty(ids) {
		asset, err := s.repo.SharedAsset(id)
		if err != nil || asset.Status != model.SharedAssetReady {
			return BadAuthRequest("引用的共享素材不可用")
		}
		series, err := s.repo.SharedAssetSeries(asset.SeriesID)
		if err != nil || series.Status != model.SharedAssetSeriesReady {
			return BadAuthRequest("引用的共享素材系列不可用")
		}
	}
	return nil
}

func collectSharedAssetIDs(value any, ids *[]string) {
	switch current := value.(type) {
	case map[string]any:
		if current["source"] == "shared" {
			if id, ok := current["sharedAssetId"].(string); ok {
				*ids = append(*ids, id)
			}
		}
		for _, child := range current {
			collectSharedAssetIDs(child, ids)
		}
	case []any:
		for _, child := range current {
			collectSharedAssetIDs(child, ids)
		}
	}
}

func (s *Service) startSharedLibraryWorker(ctx context.Context) {
	s.runWorkerLoop(func(ctx context.Context) {
		ticker := time.NewTicker(5 * time.Second)
		cleanup := time.NewTicker(time.Hour)
		defer ticker.Stop()
		defer cleanup.Stop()
		for {
			s.drainSharedZIPBatches(2)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-cleanup.C:
				s.cleanupSharedLibraryStaging()
			}
		}
	})
}

func (s *Service) drainSharedZIPBatches(limit int) {
	for index := 0; index < limit; index++ {
		batch, err := s.repo.ClaimNextSharedZIPBatch(s.workerID, sharedUploadLease)
		if err != nil || batch == nil {
			return
		}
		claimed := *batch
		if !s.runWorkerTask(func() { s.processSharedZIPBatch(&claimed) }) {
			return
		}
	}
}

func (s *Service) processSharedZIPBatch(batch *model.SharedAssetUploadBatch) {
	err := s.extractSharedZIP(batch)
	if err == nil {
		return
	}
	if batch.Attempts < 5 && !isSharedZIPBusinessError(err) {
		delays := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 10 * time.Minute, 10 * time.Minute}
		_ = s.repo.FinishSharedUploadBatch(batch.ID, s.workerID, map[string]any{"status": model.SharedBatchQueued, "error": err.Error(), "next_attempt_at": time.Now().Add(delays[min(batch.Attempts-1, len(delays)-1)])})
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	_ = s.repo.FinishSharedUploadBatch(batch.ID, s.workerID, map[string]any{"status": model.SharedBatchFailed, "error": err.Error(), "archive_expires": expires})
}

type sharedZIPBusinessError struct{ error }

func isSharedZIPBusinessError(err error) bool {
	var target sharedZIPBusinessError
	return errors.As(err, &target)
}

func (s *Service) extractSharedZIP(batch *model.SharedAssetUploadBatch) error {
	reader, err := zip.OpenReader(batch.ArchivePath)
	if err != nil {
		return sharedZIPBusinessError{fmt.Errorf("ZIP 文件损坏：%w", err)}
	}
	defer reader.Close()
	if len(reader.File) > sharedZIPMaxEntries {
		return sharedZIPBusinessError{errors.New("ZIP 条目超过 5000")}
	}
	var declared int64
	imageEntries := 0
	for _, entry := range reader.File {
		if err := validateSharedZIPEntry(entry); err != nil {
			return sharedZIPBusinessError{err}
		}
		if entry.FileInfo().IsDir() || ignoredSharedZIPEntry(entry.Name) {
			continue
		}
		declared += int64(entry.UncompressedSize64)
		if declared > sharedBatchMaxBytes {
			return sharedZIPBusinessError{errors.New("ZIP 解压后总大小超过 5GB")}
		}
		if entry.CompressedSize64 == 0 && entry.UncompressedSize64 > 0 || entry.CompressedSize64 > 0 && entry.UncompressedSize64 > entry.CompressedSize64*uint64(sharedZIPMaxRatio) {
			return sharedZIPBusinessError{fmt.Errorf("ZIP 条目压缩比超过 %d:1", sharedZIPMaxRatio)}
		}
		if allowedSharedImageName(entry.Name) {
			imageEntries++
		}
	}
	if imageEntries > sharedBatchMaxFiles {
		return sharedZIPBusinessError{errors.New("ZIP 中图片超过 1000 张")}
	}
	if imageEntries == 0 {
		return sharedZIPBusinessError{errors.New("ZIP 中没有可导入的 JPG、PNG 或 WebP 图片")}
	}
	if err := s.repo.HeartbeatSharedUploadBatch(batch.ID, s.workerID, model.SharedBatchImporting, sharedUploadLease); err != nil {
		return err
	}

	titles := map[string]int{}
	for index, entry := range reader.File {
		if entry.FileInfo().IsDir() || ignoredSharedZIPEntry(entry.Name) || !allowedSharedImageName(entry.Name) {
			continue
		}
		clientID := fmt.Sprintf("zip:%d", index)
		item, err := s.repo.SharedUploadItem(batch.ID, clientID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := time.Now()
			item = &model.SharedAssetUploadItem{ID: newID(), BatchID: batch.ID, ClientID: clientID, FileName: filepath.Base(entry.Name), ExpectedSize: int64(entry.UncompressedSize64), Status: model.SharedItemPending, CreatedAt: now, UpdatedAt: now}
			if err = s.repo.SaveSharedUploadItem(item); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if item.Status == model.SharedItemReady || item.Status == model.SharedItemSkipped {
			continue
		}
		title := strings.TrimSuffix(filepath.Base(entry.Name), filepath.Ext(entry.Name))
		titles[title]++
		if titles[title] > 1 {
			title = fmt.Sprintf("%s (%d)", title, titles[title])
		}
		if err := s.importSharedZIPEntry(batch, item, entry, title); err != nil {
			item.Status, item.Error, item.UpdatedAt = model.SharedItemSkipped, err.Error(), time.Now()
			_ = s.repo.SaveSharedUploadItem(item)
		}
		if index%20 == 0 {
			_ = s.repo.HeartbeatSharedUploadBatch(batch.ID, s.workerID, model.SharedBatchImporting, sharedUploadLease)
		}
	}
	ready, skipped, failed, processed, err := s.repo.RecountSharedUploadBatch(batch.ID)
	if err != nil {
		return err
	}
	// The archive upload row is operational metadata and is not an imported image.
	ready--
	if ready <= 0 {
		return sharedZIPBusinessError{errors.New("ZIP 中没有有效图片")}
	}
	status := model.SharedBatchCompleted
	if skipped > 0 || failed > 0 {
		status = model.SharedBatchCompletedWithErrors
	}
	series, err := s.repo.SharedAssetSeries(batch.SeriesID)
	if err != nil {
		return err
	}
	series.Status, series.UpdatedAt = model.SharedAssetSeriesReady, time.Now()
	if err := s.repo.SaveSharedAssetSeries(series); err != nil {
		return err
	}
	_ = os.Remove(batch.ArchivePath)
	return s.repo.FinishSharedUploadBatch(batch.ID, s.workerID, map[string]any{"status": status, "ready_count": ready, "skipped_count": skipped, "failed_count": failed, "processed_bytes": processed, "archive_path": "", "error": ""})
}

func (s *Service) importSharedZIPEntry(batch *model.SharedAssetUploadBatch, item *model.SharedAssetUploadItem, entry *zip.File, title string) error {
	body, err := entry.Open()
	if err != nil {
		return err
	}
	defer body.Close()
	path := s.sharedStagingPath(batch.ID, item.ID, false)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, sharedSingleMaxBytes+1))
	closeErr := file.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(path + ".part")
		return copyErr
	}
	if written > sharedSingleMaxBytes {
		_ = os.Remove(path + ".part")
		return errors.New("单张图片超过 50MB")
	}
	if err := os.Rename(path+".part", path); err != nil {
		return err
	}
	item.StagingPath, item.ActualSize, item.ActualSHA256, item.Status = path, written, hex.EncodeToString(hash.Sum(nil)), model.SharedItemVerifying
	item.FileName = filepath.Base(entry.Name)
	item.ExpectedSize = written
	item.UpdatedAt = time.Now()
	if err := s.repo.SaveSharedUploadItem(item); err != nil {
		return err
	}
	return s.importSharedStagedImageNamed(batch, item, title)
}

func (s *Service) importSharedStagedImage(batch *model.SharedAssetUploadBatch, item *model.SharedAssetUploadItem) error {
	return s.importSharedStagedImageNamed(batch, item, strings.TrimSuffix(filepath.Base(item.FileName), filepath.Ext(item.FileName)))
}

func (s *Service) importSharedStagedImageNamed(batch *model.SharedAssetUploadBatch, item *model.SharedAssetUploadItem, title string) error {
	mimeType, sum, err := verifyStagedFile(item, true)
	if err != nil {
		item.Status, item.Error = model.SharedItemFailed, err.Error()
		_ = s.repo.SaveSharedUploadItem(item)
		return err
	}
	file, err := os.Open(item.StagingPath)
	if err != nil {
		return err
	}
	config, _, configErr := image.DecodeConfig(bufio.NewReader(file))
	_, _ = file.Seek(0, io.SeekStart)
	width, height := 0, 0
	if configErr == nil {
		width, height = config.Width, config.Height
	}
	resource, err := s.storeSharedResource(batch.OwnerUserID, "image", item.FileName, mimeType, item.ActualSize, width, height, 0, file)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	now := time.Now()
	asset := &model.SharedAsset{ID: newID(), SeriesID: batch.SeriesID, UploaderUserID: batch.OwnerUserID, ResourceID: resource.ID, ThumbnailResourceID: resource.ID,
		UploadItemID: item.ID, Title: title, MimeType: mimeType, Size: item.ActualSize, Width: width, Height: height, SHA256: sum, Version: 1, Status: model.SharedAssetReady, CreatedAt: now, UpdatedAt: now}
	item.ActualSHA256, item.Status, item.UpdatedAt = sum, model.SharedItemReady, now
	if err := s.repo.CommitSharedAsset(item, asset, batch.SeriesID); err != nil {
		return err
	}
	_ = os.Remove(item.StagingPath)
	return nil
}

func (s *Service) completeSharedBatch(batch *model.SharedAssetUploadBatch, errorText string) error {
	ready, skipped, failed, processed, err := s.repo.RecountSharedUploadBatch(batch.ID)
	if err != nil {
		return err
	}
	status := model.SharedBatchCompleted
	if skipped > 0 || failed > 0 {
		status = model.SharedBatchCompletedWithErrors
	}
	batch.Status, batch.ReadyCount, batch.SkippedCount, batch.FailedCount, batch.ProcessedBytes, batch.Error, batch.UpdatedAt = status, int(ready), int(skipped), int(failed), processed, errorText, time.Now()
	return s.repo.SaveSharedUploadBatch(batch)
}

func verifyStagedFile(item *model.SharedAssetUploadItem, imageOnly bool) (string, string, error) {
	file, err := os.Open(item.StagingPath)
	if err != nil {
		return "", "", BadAuthRequest("上传文件不存在，请重新上传")
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	if stat.Size() != item.ExpectedSize {
		return "", "", BadAuthRequest("实际文件大小与上传清单不一致")
	}
	hash := sha256.New()
	prefix := make([]byte, 512)
	n, readErr := io.ReadFull(file, prefix)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", "", readErr
	}
	if _, err := hash.Write(prefix[:n]); err != nil {
		return "", "", err
	}
	if _, err := io.Copy(hash, file); err != nil {
		return "", "", err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if item.ExpectedSHA256 != "" && !strings.EqualFold(sum, item.ExpectedSHA256) {
		return "", "", BadAuthRequest("SHA-256 校验失败")
	}
	mimeType := http.DetectContentType(prefix[:n])
	if imageOnly && !allowedSharedMime(mimeType) {
		return "", "", BadAuthRequest("文件头不是受支持的 JPG、PNG 或 WebP 图片")
	}
	item.ActualSize, item.ActualSHA256 = stat.Size(), sum
	return mimeType, sum, nil
}

func validateSharedZIPEntry(entry *zip.File) error {
	name := strings.ReplaceAll(entry.Name, "\\", "/")
	clean := filepath.ToSlash(filepath.Clean(name))
	if entry.Flags&0x1 != 0 {
		return errors.New("不支持加密 ZIP")
	}
	firstPart := strings.SplitN(clean, "/", 2)[0]
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.Contains(firstPart, ":") || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("ZIP 包含不安全路径")
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return errors.New("ZIP 包含符号链接")
	}
	return nil
}

func ignoredSharedZIPEntry(name string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
		if part == "__MACOSX" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func allowedSharedImageName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
}
func allowedSharedMime(value string) bool {
	value, _, _ = mime.ParseMediaType(value)
	return value == "image/jpeg" || value == "image/png" || value == "image/webp"
}
func isHex(value string) bool { _, err := hex.DecodeString(value); return err == nil }

func (s *Service) sharedStagingPath(batchID, itemID string, archive bool) string {
	name := itemID + ".upload"
	if archive {
		name = "source.zip"
	}
	return filepath.Join(s.dataDir, "shared-library", "staging", safeObjectSegment(batchID), name)
}

func (s *Service) cleanupSharedLibraryStaging() {
	root := filepath.Join(s.dataDir, "shared-library", "staging")
	entries, _ := os.ReadDir(root)
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
	_ = s.repo.CleanupExpiredSharedUploadRows(time.Now().Add(-30 * 24 * time.Hour))
}

func sharedJSON(value any) string { payload, _ := json.Marshal(value); return string(payload) }
