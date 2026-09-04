package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const CurrentSchemaVersion int64 = 7

const baselineSchemaChecksum = "sha256:open-ai-canvas-schema-v1-20260830"
const schemaMigrationAppliedAtIndexChecksum = "sha256:schema-migrations-applied-at-index-v2-20260830"
const assetTaxonomyCandidateIdentityChecksum = "sha256:asset-taxonomy-candidate-identity-v3-20260831-r1"
const sharedAssetLibraryChecksum = "sha256:shared-asset-library-v4-20260902"
const sharedAssetStorageScopeChecksum = "sha256:shared-asset-storage-scope-v5-20260903"
const legacyZQAssetPayloadChecksum = "sha256:legacy-zq-asset-client-payload-v6-20260903"
const sharedAssetSeriesHierarchyChecksum = "sha256:shared-asset-series-hierarchy-v7-20260904"

const postgresSchemaMigrationLockID int64 = 73123910420260830

type SchemaStatus struct {
	Current  int64 `json:"current"`
	Expected int64 `json:"expected"`
	Ready    bool  `json:"ready"`
}

type schemaMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"size:160;not null"`
	Checksum  string    `gorm:"size:96;not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string { return "schema_migrations" }

type migration struct {
	version  int64
	name     string
	checksum string
	apply    func(*gorm.DB) error
}

var schemaMigrations = []migration{
	{version: 1, name: "baseline_gorm_schema", checksum: baselineSchemaChecksum, apply: migrateSchemaV1},
	{version: 2, name: "schema_migrations_applied_at_index", checksum: schemaMigrationAppliedAtIndexChecksum, apply: migrateSchemaV2},
	{version: 3, name: "asset_taxonomy_candidate_identity", checksum: assetTaxonomyCandidateIdentityChecksum, apply: migrateSchemaV3},
	{version: 4, name: "shared_asset_library", checksum: sharedAssetLibraryChecksum, apply: migrateSchemaV4},
	{version: 5, name: "shared_asset_storage_scope", checksum: sharedAssetStorageScopeChecksum, apply: migrateSchemaV5},
	{version: 6, name: "legacy_zq_asset_client_payload", checksum: legacyZQAssetPayloadChecksum, apply: migrateSchemaV6},
	{version: 7, name: "shared_asset_series_hierarchy", checksum: sharedAssetSeriesHierarchyChecksum, apply: migrateSchemaV7},
}

func migrateSchemaV7(tx *gorm.DB) error {
	return tx.AutoMigrate(&model.SharedAssetSeries{})
}

func migrateSchemaV6(tx *gorm.DB) error {
	var assets []model.Asset
	if err := tx.Where("id LIKE ?", "zqa_%").Find(&assets).Error; err != nil {
		return fmt.Errorf("读取历史 ZQ 素材：%w", err)
	}
	for _, asset := range assets {
		payload := make(map[string]any)
		if err := json.Unmarshal([]byte(asset.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("解析历史 ZQ 素材 %s：%w", asset.ID, err)
		}
		if data, ok := payload["data"].(map[string]any); ok && data != nil {
			continue
		}

		var representation model.AssetRepresentation
		if err := tx.Where("asset_version_id = ? AND role = ?", asset.PrimaryVersionID, "original").Order("created_at asc").First(&representation).Error; err != nil {
			return fmt.Errorf("读取历史 ZQ 素材 %s 的原始表现：%w", asset.ID, err)
		}
		var resource model.Resource
		if err := tx.First(&resource, "id = ?", representation.ResourceID).Error; err != nil {
			return fmt.Errorf("读取历史 ZQ 素材 %s 的资源：%w", asset.ID, err)
		}

		kind := legacyClientAssetKind(asset.Kind, resource.MimeType)
		resourceURL := "/api/resources/" + resource.ID + "/file"
		data := map[string]any{
			"storageKey": "resource:" + resource.ID,
			"bytes":      resource.Size,
			"mimeType":   resource.MimeType,
		}
		coverURL := ""
		switch kind {
		case "video":
			data["url"] = resourceURL
			data["width"] = resource.Width
			data["height"] = resource.Height
			data["durationMs"] = resource.DurationMs
		case "audio":
			data["url"] = resourceURL
			data["durationMs"] = resource.DurationMs
		default:
			data["dataUrl"] = resourceURL
			data["width"] = resource.Width
			data["height"] = resource.Height
			coverURL = resourceURL
		}

		payload["id"] = asset.ID
		payload["kind"] = kind
		payload["title"] = asset.Title
		payload["coverUrl"] = coverURL
		if _, ok := payload["tags"]; !ok {
			payload["tags"] = []string{}
		}
		payload["category"] = model.NormalizeAssetCategory(asset.Category, kind)
		payload["status"] = asset.Status
		payload["primaryVersionId"] = asset.PrimaryVersionID
		payload["createdAt"] = asset.CreatedAt.UTC().Format(time.RFC3339Nano)
		payload["updatedAt"] = asset.UpdatedAt.UTC().Format(time.RFC3339Nano)
		payload["data"] = data
		normalized, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("生成历史 ZQ 素材 %s 的客户端载荷：%w", asset.ID, err)
		}
		if err := tx.Model(&model.Asset{}).Where("id = ?", asset.ID).Updates(map[string]any{
			"kind": kind, "category": model.NormalizeAssetCategory(asset.Category, kind), "payload_json": string(normalized),
		}).Error; err != nil {
			return fmt.Errorf("修复历史 ZQ 素材 %s：%w", asset.ID, err)
		}
	}
	return nil
}

func legacyClientAssetKind(kind string, mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return "video"
	case "audio":
		return "audio"
	case "image":
		return "image"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "video/") {
		return "video"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "audio/") {
		return "audio"
	}
	return "image"
}

func migrateSchemaV5(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE resources
		SET source_system = ?
		WHERE id IN (
			SELECT resource_id FROM shared_assets
			UNION
			SELECT thumbnail_resource_id FROM shared_assets WHERE thumbnail_resource_id <> ''
		)
	`, model.SharedLibraryResourceSourceSystem).Error
}

func migrateSchemaV4(tx *gorm.DB) error {
	return tx.AutoMigrate(
		&model.User{},
		&model.SharedAssetSeries{},
		&model.SharedAsset{},
		&model.SharedAssetUploadBatch{},
		&model.SharedAssetUploadItem{},
		&model.ProjectSharedAssetLink{},
	)
}

func migrateSchemaV2(tx *gorm.DB) error {
	return tx.Exec("CREATE INDEX IF NOT EXISTS idx_schema_migrations_applied_at ON schema_migrations (applied_at)").Error
}

func migrateSchemaV3(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.ProjectAssetCandidate{}); err != nil {
		return fmt.Errorf("扩展资产候选身份字段：%w", err)
	}
	if err := tx.Exec("UPDATE assets SET category = 'prop' WHERE category IN ('wardrobe', 'weapon', 'accessory')").Error; err != nil {
		return fmt.Errorf("合并资产道具分类：%w", err)
	}
	if err := tx.Exec("UPDATE assets SET category = 'material' WHERE category = 'style' OR (category = 'other' AND kind IN ('image', 'video', 'audio', 'model'))").Error; err != nil {
		return fmt.Errorf("迁移资产素材分类：%w", err)
	}
	if err := tx.Exec("UPDATE project_asset_candidates SET category = 'prop' WHERE category IN ('wardrobe', 'weapon', 'accessory')").Error; err != nil {
		return fmt.Errorf("合并候选道具分类：%w", err)
	}
	if err := tx.Exec("UPDATE project_asset_candidates SET category = 'material' WHERE category = 'style'").Error; err != nil {
		return fmt.Errorf("迁移候选素材分类：%w", err)
	}
	var candidates []model.ProjectAssetCandidate
	if err := tx.Order("created_at asc, id asc").Find(&candidates).Error; err != nil {
		return fmt.Errorf("读取资产候选身份：%w", err)
	}
	seenPending := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		nameKey := model.AssetCandidateNameKey(candidate.Name)
		updates := map[string]any{"name_key": nameKey}
		identity := candidate.ProjectID + ":" + string(candidate.Category) + ":" + nameKey
		if candidate.Status == "pending_confirmation" && nameKey != "" {
			if _, exists := seenPending[identity]; exists {
				updates["status"] = "ignored"
			} else {
				seenPending[identity] = candidate.ID
			}
		}
		if err := tx.Model(&model.ProjectAssetCandidate{}).Where("id = ?", candidate.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("回填资产候选身份 %s：%w", candidate.ID, err)
		}
	}
	return tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_project_asset_candidates_pending_identity ON project_asset_candidates(project_id, category, name_key) WHERE status = 'pending_confirmation' AND name_key <> ''").Error
}

func MigrateSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", postgresSchemaMigrationLockID).Error; err != nil {
				return fmt.Errorf("获取数据库迁移锁：%w", err)
			}
		}
		if err := tx.AutoMigrate(&schemaMigration{}); err != nil {
			return fmt.Errorf("初始化数据库迁移记录：%w", err)
		}
		for _, item := range schemaMigrations {
			var applied schemaMigration
			err := tx.First(&applied, "version = ?", item.version).Error
			if err == nil {
				if err := validateMigrationRecord(applied, item); err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("读取数据库迁移 %d：%w", item.version, err)
			}
			if err := item.apply(tx); err != nil {
				return fmt.Errorf("执行数据库迁移 %d（%s）：%w", item.version, item.name, err)
			}
			record := schemaMigration{Version: item.version, Name: item.name, Checksum: item.checksum, AppliedAt: time.Now().UTC()}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("记录数据库迁移 %d：%w", item.version, err)
			}
		}
		return RequireSchemaVersion(tx)
	})
}

func ReadSchemaStatus(db *gorm.DB) (SchemaStatus, error) {
	status := SchemaStatus{Expected: CurrentSchemaVersion}
	if !db.Migrator().HasTable(&schemaMigration{}) {
		return status, nil
	}
	if err := db.Model(&schemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&status.Current).Error; err != nil {
		return status, fmt.Errorf("读取数据库结构版本：%w", err)
	}
	if status.Current != status.Expected {
		return status, nil
	}
	if err := validateMigrationRecords(db); err != nil {
		return status, err
	}
	status.Ready = true
	return status, nil
}

func validateMigrationRecords(db *gorm.DB) error {
	for _, item := range schemaMigrations {
		var applied schemaMigration
		if err := db.First(&applied, "version = ?", item.version).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("数据库缺少迁移记录 %d（%s）", item.version, item.name)
			}
			return fmt.Errorf("读取数据库迁移 %d：%w", item.version, err)
		}
		if err := validateMigrationRecord(applied, item); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationRecord(applied schemaMigration, expected migration) error {
	if applied.Name != expected.name {
		return fmt.Errorf("数据库迁移 %d 名称不一致：记录为 %s，程序期望 %s", expected.version, applied.Name, expected.name)
	}
	if applied.Checksum != expected.checksum {
		return fmt.Errorf("数据库迁移 %d 校验和不一致：记录为 %s，程序期望 %s", expected.version, applied.Checksum, expected.checksum)
	}
	return nil
}

func RequireSchemaVersion(db *gorm.DB) error {
	status, err := ReadSchemaStatus(db)
	if err != nil {
		return err
	}
	if status.Current < status.Expected {
		return fmt.Errorf("数据库结构版本过旧：当前 %d，程序要求 %d，请先执行 migrate-schema up", status.Current, status.Expected)
	}
	if status.Current > status.Expected {
		return fmt.Errorf("数据库结构版本 %d 高于程序支持的 %d，拒绝使用旧程序连接新数据库", status.Current, status.Expected)
	}
	return nil
}
