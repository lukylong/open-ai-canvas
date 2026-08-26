package zqmigration

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/service"

	"gorm.io/gorm"
)

const platformOSSSettingKey = "oss"

type PlatformStorageOptions struct {
	ActorUserID string
	PathPrefix  string
	Replace     bool
}

type PlatformStorageResult struct {
	Status             string    `json:"status"`
	Provider           string    `json:"provider"`
	Region             string    `json:"region"`
	Endpoint           string    `json:"endpoint"`
	CDNBaseURL         string    `json:"cdnBaseUrl"`
	Bucket             string    `json:"bucket"`
	PathPrefix         string    `json:"pathPrefix"`
	HasAccessKeyID     bool      `json:"hasAccessKeyId"`
	HasAccessKeySecret bool      `json:"hasAccessKeySecret"`
	UpdatedBy          string    `json:"updatedBy"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// ImportPlatformStorage imports the source COS connection once. Existing
// platform storage is preserved unless Replace is explicitly requested so a
// later incremental data sync cannot overwrite an administrator's changes.
func (m *Migrator) ImportPlatformStorage(options PlatformStorageOptions) (PlatformStorageResult, error) {
	if m == nil || m.target == nil || m.svc == nil {
		return PlatformStorageResult{}, errors.New("Canvas 目标数据库未初始化")
	}
	actor, err := m.platformStorageActor(strings.TrimSpace(options.ActorUserID))
	if err != nil {
		return PlatformStorageResult{}, err
	}
	var existing model.SystemSetting
	existingErr := m.target.First(&existing, "key = ?", platformOSSSettingKey).Error
	if existingErr == nil && !options.Replace {
		current, readErr := m.svc.AdminOSSSetting(actor)
		if readErr != nil {
			return PlatformStorageResult{}, readErr
		}
		return platformStorageResult("preserved", current), nil
	}
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return PlatformStorageResult{}, existingErr
	}

	config := m.cos.normalize()
	if config.Bucket == "" {
		return PlatformStorageResult{}, errors.New("ZQ QCLOUD_COS_BUCKET 为空")
	}
	if config.AccessKeyID == "" {
		return PlatformStorageResult{}, errors.New("ZQ QCLOUD_COS_SECRET_ID/QCLOUD_COS_ACCESS_KEY 为空")
	}
	if config.AccessKeySecret == "" {
		return PlatformStorageResult{}, errors.New("ZQ QCLOUD_COS_SECRET_KEY 为空")
	}
	endpoint := firstNonEmptyString(config.InternalEndpoint, config.Domain)
	if endpoint == "" {
		endpoint = "https://cos." + config.Region + ".myqcloud.com"
	}
	cdnBaseURL := ""
	if config.PublicRead {
		cdnBaseURL = config.Domain
	}
	pathPrefix := strings.Trim(strings.TrimSpace(options.PathPrefix), "/")
	if pathPrefix == "" {
		pathPrefix = "canvas"
	}
	updated, err := m.svc.UpdateOSSSetting(actor, service.OSSSettingRequest{
		Enabled:         true,
		Provider:        "tencent",
		Region:          config.Region,
		Endpoint:        endpoint,
		CDNBaseURL:      cdnBaseURL,
		Bucket:          config.Bucket,
		AccessKeyID:     config.AccessKeyID,
		AccessKeySecret: config.AccessKeySecret,
		PathPrefix:      pathPrefix,
	})
	if err != nil {
		return PlatformStorageResult{}, fmt.Errorf("写入 Canvas 平台 COS 配置失败: %w", err)
	}
	status := "created"
	if existingErr == nil {
		status = "updated"
	}
	return platformStorageResult(status, updated), nil
}

func (m *Migrator) platformStorageActor(actorUserID string) (*model.User, error) {
	var actor model.User
	query := m.target.Where("role = ? AND status = ?", model.UserRoleAdmin, model.UserStatusActive)
	if actorUserID != "" {
		query = query.Where("id = ?", actorUserID)
	}
	if err := query.Order("created_at asc, id asc").First(&actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Canvas 中没有可用于记录配置变更的启用管理员")
		}
		return nil, err
	}
	return &actor, nil
}

func platformStorageResult(status string, setting *service.PublicOSSSetting) PlatformStorageResult {
	if setting == nil {
		return PlatformStorageResult{Status: status}
	}
	return PlatformStorageResult{
		Status:             status,
		Provider:           setting.Provider,
		Region:             setting.Region,
		Endpoint:           setting.Endpoint,
		CDNBaseURL:         setting.CDNBaseURL,
		Bucket:             setting.Bucket,
		PathPrefix:         setting.PathPrefix,
		HasAccessKeyID:     strings.TrimSpace(setting.AccessKeyID) != "",
		HasAccessKeySecret: setting.HasAccessKeySecret,
		UpdatedBy:          setting.UpdatedBy,
		UpdatedAt:          setting.UpdatedAt,
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
