package zqmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sourceSystem = "zq-media-studio"

type Migrator struct {
	source *gorm.DB
	target *gorm.DB
	svc    *service.Service
	cos    COSConfig
	runID  string
}

type Stats struct {
	Scanned   int64 `json:"scanned"`
	Imported  int64 `json:"imported"`
	Merged    int64 `json:"merged"`
	Conflicts int64 `json:"conflicts"`
	Skipped   int64 `json:"skipped"`
}

type Inventory struct {
	Accounts             int64 `json:"accounts"`
	Assets               int64 `json:"assets"`
	VoiceProfiles        int64 `json:"voiceProfiles"`
	InvitationCodes      int64 `json:"invitationCodes"`
	InvitationCodeUsages int64 `json:"invitationCodeUsages"`
}

type Verification struct {
	Source    Inventory        `json:"source"`
	Mapped    map[string]int64 `json:"mapped"`
	Missing   map[string]int64 `json:"missing"`
	Conflicts map[string]int64 `json:"conflicts"`
}

func New(source *gorm.DB, target *gorm.DB, dataDir string, cos COSConfig) *Migrator {
	repo := repository.New(target)
	return &Migrator{source: source, target: target, svc: service.New(repo, dataDir), cos: cos.normalize()}
}

func (m *Migrator) Inventory() (Inventory, error) {
	result := Inventory{}
	queries := []struct {
		model any
		where string
		value *int64
	}{
		{&SourceAccount{}, "is_deleted = false", &result.Accounts},
		{&SourceAsset{}, "is_deleted = false", &result.Assets},
		{&SourceVoiceProfile{}, "is_deleted = false", &result.VoiceProfiles},
		{&SourceInvitationCode{}, "is_deleted = false", &result.InvitationCodes},
		{&SourceInvitationUsage{}, "is_deleted = false", &result.InvitationCodeUsages},
	}
	for _, query := range queries {
		if err := m.source.Model(query.model).Where(query.where).Count(query.value).Error; err != nil {
			return Inventory{}, err
		}
	}
	return result, nil
}

func (m *Migrator) Backfill() (Stats, error) {
	return m.RunOnce(time.Time{})
}

// RunOnce reads an overlap window and relies on MigrationEntityMap for idempotency.
func (m *Migrator) RunOnce(since time.Time) (stats Stats, err error) {
	run := model.MigrationRun{ID: deterministicID("zqrun_", fmt.Sprintf("%d", time.Now().UnixNano())), SourceSystem: sourceSystem, Mode: "sync", Status: model.MigrationRunRunning, Watermark: since, StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err = m.target.Create(&run).Error; err != nil {
		return stats, err
	}
	m.runID = run.ID
	defer func() {
		defer func() { m.runID = "" }()
		finished := time.Now()
		updates := map[string]any{
			"status": model.MigrationRunSucceeded, "finished_at": &finished, "updated_at": finished,
			"watermark":     run.Watermark,
			"scanned_count": stats.Scanned, "imported_count": stats.Imported, "merged_count": stats.Merged,
			"conflict_count": stats.Conflicts, "skipped_count": stats.Skipped,
		}
		if err != nil {
			updates["status"] = model.MigrationRunFailed
			updates["error"] = err.Error()
		}
		_ = m.target.Model(&model.MigrationRun{}).Where("id = ?", run.ID).Updates(updates).Error
	}()

	steps := []func(time.Time, *Stats) error{
		m.processAccounts,
		m.processInvitationCodes,
		m.processInvitationUsages,
		m.processAssets,
		m.processVoiceProfiles,
	}
	for _, step := range steps {
		if err = step(since, &stats); err != nil {
			return stats, err
		}
	}
	run.Watermark, err = m.latestSourceWatermark()
	if err != nil {
		return stats, fmt.Errorf("读取 ZQ 最新水位：%w", err)
	}
	return stats, nil
}

func (m *Migrator) LastSuccessfulWatermark() (time.Time, error) {
	var run model.MigrationRun
	err := m.target.Where("source_system = ? AND status = ?", sourceSystem, model.MigrationRunSucceeded).Order("finished_at DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	return run.Watermark, err
}

func (m *Migrator) latestSourceWatermark() (time.Time, error) {
	latest := time.Time{}
	for _, table := range []string{"accounts", "invitation_codes", "invitation_code_usages", "assets", "voice_profiles"} {
		var values []time.Time
		if err := m.source.Table(table).Order("sys_update_datetime DESC").Limit(1).Pluck("sys_update_datetime", &values).Error; err != nil {
			return time.Time{}, err
		}
		if len(values) > 0 && values[0].After(latest) {
			latest = values[0]
		}
	}
	return latest, nil
}

func (m *Migrator) Verify() (Verification, error) {
	source, err := m.Inventory()
	if err != nil {
		return Verification{}, err
	}
	result := Verification{Source: source, Mapped: map[string]int64{}, Missing: map[string]int64{}, Conflicts: map[string]int64{}}
	entities := []struct {
		entityType string
		source     any
		expected   int64
	}{
		{"account", &SourceAccount{}, source.Accounts},
		{"asset", &SourceAsset{}, source.Assets},
		{"voice_profile", &SourceVoiceProfile{}, source.VoiceProfiles},
		{"invitation_code", &SourceInvitationCode{}, source.InvitationCodes},
		{"invitation_usage", &SourceInvitationUsage{}, source.InvitationCodeUsages},
	}
	for _, entity := range entities {
		var sourceIDs []string
		if err := m.source.Model(entity.source).Where("is_deleted = false").Pluck("id", &sourceIDs).Error; err != nil {
			return Verification{}, err
		}
		var mapped int64
		var conflicts int64
		for start := 0; start < len(sourceIDs); start += 500 {
			end := min(start+500, len(sourceIDs))
			batch := sourceIDs[start:end]
			query := m.target.Model(&model.MigrationEntityMap{}).Where("source_system = ? AND entity_type = ? AND source_id IN ?", sourceSystem, entity.entityType, batch)
			var batchMapped, batchConflicts int64
			if err := query.Count(&batchMapped).Error; err != nil {
				return Verification{}, err
			}
			if err := m.target.Model(&model.MigrationEntityMap{}).
				Where("source_system = ? AND entity_type = ? AND source_id IN ? AND status = ?", sourceSystem, entity.entityType, batch, model.MigrationEntityConflict).
				Count(&batchConflicts).Error; err != nil {
				return Verification{}, err
			}
			mapped += batchMapped
			conflicts += batchConflicts
		}
		result.Mapped[entity.entityType] = mapped
		result.Conflicts[entity.entityType] = conflicts
		if mapped < entity.expected {
			result.Missing[entity.entityType] = entity.expected - mapped
		} else {
			result.Missing[entity.entityType] = 0
		}
	}
	return result, nil
}

func (m *Migrator) latestMap(entityType string, sourceID string) (*model.MigrationEntityMap, error) {
	var item model.MigrationEntityMap
	err := m.target.Where("source_system = ? AND entity_type = ? AND source_id = ?", sourceSystem, entityType, sourceID).First(&item).Error
	return &item, err
}

func checksum(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m *Migrator) saveMap(entityType string, sourceID string, targetID string, status model.MigrationEntityStatus, updatedAt time.Time, digest string, details any) error {
	detailsJSON := ""
	if details != nil {
		raw, _ := json.Marshal(details)
		detailsJSON = string(raw)
	}
	now := time.Now()
	item := model.MigrationEntityMap{ID: deterministicID("zqmap_", entityType+":"+sourceID), SourceSystem: sourceSystem, EntityType: entityType, SourceID: sourceID, TargetID: targetID, Status: status, SourceUpdated: updatedAt, Checksum: digest, DetailsJSON: detailsJSON, CreatedAt: now, UpdatedAt: now}
	if err := m.target.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_system"}, {Name: "entity_type"}, {Name: "source_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"target_id", "status", "source_updated", "checksum", "details_json", "updated_at"}),
	}).Create(&item).Error; err != nil {
		return err
	}
	if status != model.MigrationEntityConflict {
		return m.target.Model(&model.MigrationConflict{}).
			Where("source_system = ? AND entity_type = ? AND source_id = ? AND resolved_at IS NULL", sourceSystem, entityType, sourceID).
			Update("resolved_at", now).Error
	}
	return nil
}

func (m *Migrator) conflict(entityType string, sourceID string, reason string, updatedAt time.Time, digest string, details any) error {
	raw, _ := json.Marshal(details)
	now := time.Now()
	conflict := model.MigrationConflict{ID: deterministicID("zqconf_", entityType+":"+sourceID+":"+reason), RunID: m.runID, SourceSystem: sourceSystem, EntityType: entityType, SourceID: sourceID, Reason: reason, DetailsJSON: string(raw), CreatedAt: now}
	if err := m.target.Clauses(clause.OnConflict{DoNothing: true}).Create(&conflict).Error; err != nil {
		return err
	}
	return m.saveMap(entityType, sourceID, "", model.MigrationEntityConflict, updatedAt, digest, map[string]any{"reason": reason})
}

func updateStats(stats *Stats, status model.MigrationEntityStatus) {
	stats.Scanned++
	switch status {
	case model.MigrationEntityImported:
		stats.Imported++
	case model.MigrationEntityMerged:
		stats.Merged++
	case model.MigrationEntityConflict:
		stats.Conflicts++
	default:
		stats.Skipped++
	}
}

func incrementalQuery(db *gorm.DB, since time.Time) *gorm.DB {
	if since.IsZero() {
		return db
	}
	return db.Where("sys_update_datetime >= ?", since)
}

func activeUserStatus(source SourceAccount) model.UserStatus {
	if source.IsDeleted || !strings.EqualFold(strings.TrimSpace(source.Status), "active") {
		return model.UserStatusDisabled
	}
	return model.UserStatusActive
}

func (m *Migrator) processAccounts(since time.Time, stats *Stats) error {
	var rows []SourceAccount
	if err := incrementalQuery(m.source.Model(&SourceAccount{}), since).Order("sys_update_datetime, id").Find(&rows).Error; err != nil {
		return fmt.Errorf("读取 ZQ 用户：%w", err)
	}
	for _, source := range rows {
		digest := checksum(source)
		mapping, mapErr := m.latestMap("account", source.ID)
		if mapErr == nil && (mapping.Status == model.MigrationEntityImported || mapping.Status == model.MigrationEntityMerged) {
			var target model.User
			if err := m.target.First(&target, "id = ?", mapping.TargetID).Error; err == nil {
				if mapping.Status == model.MigrationEntityImported && target.SourceSystem == sourceSystem {
					updates := map[string]any{"display_name": source.DisplayName, "avatar_url": source.AvatarURL, "password_hash": source.PasswordHash, "status": activeUserStatus(source), "last_login_at": source.LastLoginAt, "updated_at": source.SysUpdateDatetime}
					if err := m.target.Model(&target).Updates(updates).Error; err != nil {
						return err
					}
				} else if target.AvatarURL == "" && source.AvatarURL != "" {
					if err := m.target.Model(&target).Updates(map[string]any{"avatar_url": source.AvatarURL, "updated_at": time.Now()}).Error; err != nil {
						return err
					}
				}
				if err := m.saveMap("account", source.ID, target.ID, mapping.Status, source.SysUpdateDatetime, digest, nil); err != nil {
					return err
				}
				if err := m.svc.GrantSignupBonusForMigratedUser(target.ID); err != nil {
					return err
				}
				updateStats(stats, mapping.Status)
				continue
			}
		}

		username := strings.TrimSpace(source.Username)
		email := strings.ToLower(strings.TrimSpace(source.Email))
		var byUsername, byEmail model.User
		usernameErr := m.target.Where("lower(username) = lower(?)", username).First(&byUsername).Error
		emailErr := gorm.ErrRecordNotFound
		if email != "" {
			emailErr = m.target.Where("email <> '' AND lower(email) = lower(?)", email).First(&byEmail).Error
		}
		if !errors.Is(usernameErr, gorm.ErrRecordNotFound) || !errors.Is(emailErr, gorm.ErrRecordNotFound) {
			if usernameErr == nil && email != "" && emailErr == nil && byUsername.ID == byEmail.ID {
				if err := m.saveMap("account", source.ID, byUsername.ID, model.MigrationEntityMerged, source.SysUpdateDatetime, digest, map[string]any{"passwordPolicy": "target_preserved"}); err != nil {
					return err
				}
				if err := m.svc.GrantSignupBonusForMigratedUser(byUsername.ID); err != nil {
					return err
				}
				updateStats(stats, model.MigrationEntityMerged)
				continue
			}
			if err := m.conflict("account", source.ID, "username_or_email_collision", source.SysUpdateDatetime, digest, map[string]any{"usernameCollision": usernameErr == nil, "emailCollision": emailErr == nil}); err != nil {
				return err
			}
			updateStats(stats, model.MigrationEntityConflict)
			continue
		}
		if usernameErr != nil && !errors.Is(usernameErr, gorm.ErrRecordNotFound) {
			return usernameErr
		}
		if emailErr != nil && !errors.Is(emailErr, gorm.ErrRecordNotFound) {
			return emailErr
		}
		target := model.User{ID: deterministicID("zqu_", source.ID), Username: username, Email: email, DisplayName: source.DisplayName, AvatarURL: source.AvatarURL, SourceSystem: sourceSystem, Role: model.UserRoleUser, Status: activeUserStatus(source), PasswordHash: source.PasswordHash, LastLoginAt: source.LastLoginAt, CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
		if strings.TrimSpace(target.DisplayName) == "" {
			target.DisplayName = username
		}
		if err := m.target.Create(&target).Error; err != nil {
			return fmt.Errorf("写入 ZQ 用户 %s：%w", source.ID, err)
		}
		if err := m.saveMap("account", source.ID, target.ID, model.MigrationEntityImported, source.SysUpdateDatetime, digest, nil); err != nil {
			return err
		}
		if err := m.svc.GrantSignupBonusForMigratedUser(target.ID); err != nil {
			return err
		}
		updateStats(stats, model.MigrationEntityImported)
	}
	return nil
}

func (m *Migrator) processInvitationCodes(since time.Time, stats *Stats) error {
	var rows []SourceInvitationCode
	if err := incrementalQuery(m.source.Model(&SourceInvitationCode{}), since).Order("sys_update_datetime, id").Find(&rows).Error; err != nil {
		return err
	}
	for _, source := range rows {
		digest := checksum(source)
		hash := invitationHash(source.Code)
		var existing model.InvitationCode
		if err := m.target.Where("code_hash = ?", hash).First(&existing).Error; err == nil {
			if err := m.saveMap("invitation_code", source.ID, existing.ID, model.MigrationEntityMerged, source.SysUpdateDatetime, digest, nil); err != nil {
				return err
			}
			updateStats(stats, model.MigrationEntityMerged)
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		preview := source.Code
		if len(preview) > 6 {
			preview = "…" + preview[len(preview)-6:]
		}
		revokedAt := source.RevokedAt
		if (source.IsDeleted || !source.IsActive) && revokedAt == nil {
			now := source.SysUpdateDatetime
			revokedAt = &now
		}
		invite := model.InvitationCode{ID: deterministicID("zqi_", source.ID), CodeHash: hash, CodePreview: preview, Label: source.Label, MaxUses: source.MaxUses, UsedCount: source.UsedCount, ExpiresAt: source.ExpiresAt, RevokedAt: revokedAt, CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
		if err := m.target.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"label", "max_uses", "used_count", "expires_at", "revoked_at", "updated_at"})}).Create(&invite).Error; err != nil {
			return err
		}
		status := model.MigrationEntityImported
		if source.IsDeleted {
			status = model.MigrationEntitySkipped
		}
		if err := m.saveMap("invitation_code", source.ID, invite.ID, status, source.SysUpdateDatetime, digest, nil); err != nil {
			return err
		}
		updateStats(stats, status)
	}
	return nil
}

func (m *Migrator) processInvitationUsages(since time.Time, stats *Stats) error {
	var rows []SourceInvitationUsage
	if err := incrementalQuery(m.source.Model(&SourceInvitationUsage{}), since).Order("sys_update_datetime, id").Find(&rows).Error; err != nil {
		return err
	}
	for _, source := range rows {
		digest := checksum(source)
		inviteMap, inviteErr := m.latestMap("invitation_code", source.InvitationCodeID)
		userMap, userErr := m.latestMap("account", source.AccountID)
		if inviteErr != nil || userErr != nil || inviteMap.TargetID == "" || userMap.TargetID == "" {
			if err := m.conflict("invitation_usage", source.ID, "missing_parent_mapping", source.SysUpdateDatetime, digest, nil); err != nil {
				return err
			}
			updateStats(stats, model.MigrationEntityConflict)
			continue
		}
		usage := model.InvitationCodeUsage{ID: deterministicID("zqiu_", source.ID), InvitationCodeID: inviteMap.TargetID, UserID: userMap.TargetID, CreatedAt: source.UsedAt}
		if err := m.target.Clauses(clause.OnConflict{DoNothing: true}).Create(&usage).Error; err != nil {
			return err
		}
		status := model.MigrationEntityImported
		if source.IsDeleted {
			status = model.MigrationEntitySkipped
		}
		if err := m.saveMap("invitation_usage", source.ID, usage.ID, status, source.SysUpdateDatetime, digest, nil); err != nil {
			return err
		}
		updateStats(stats, status)
	}
	return nil
}

func (m *Migrator) processAssets(since time.Time, stats *Stats) error {
	var rows []SourceAsset
	if err := incrementalQuery(m.source.Model(&SourceAsset{}), since).Order("sys_update_datetime, id").Find(&rows).Error; err != nil {
		return fmt.Errorf("读取 ZQ 素材：%w", err)
	}
	taskIDs := make([]string, 0, len(rows))
	seenTaskIDs := make(map[string]struct{}, len(rows))
	for _, source := range rows {
		taskID := strings.TrimSpace(source.GenerationTaskID)
		if taskID == "" {
			continue
		}
		if _, exists := seenTaskIDs[taskID]; exists {
			continue
		}
		seenTaskIDs[taskID] = struct{}{}
		taskIDs = append(taskIDs, taskID)
	}
	tasksByID := make(map[string]SourceGenerationTask, len(taskIDs))
	if len(taskIDs) > 0 {
		var tasks []SourceGenerationTask
		if err := m.source.Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
			return fmt.Errorf("读取 ZQ 素材任务血缘：%w", err)
		}
		for _, task := range tasks {
			tasksByID[task.ID] = task
		}
	}
	for _, source := range rows {
		lineage := tasksByID[source.GenerationTaskID]
		digest := checksum(struct {
			Asset SourceAsset
			Task  SourceGenerationTask
		}{Asset: source, Task: lineage})
		userMap, err := m.latestMap("account", source.AccountID)
		if err != nil || userMap.TargetID == "" || userMap.Status == model.MigrationEntityConflict {
			if err := m.conflict("asset", source.ID, "owner_not_migrated", source.SysUpdateDatetime, digest, map[string]any{"sourceAccountId": source.AccountID}); err != nil {
				return err
			}
			updateStats(stats, model.MigrationEntityConflict)
			continue
		}
		assetID := deterministicID("zqa_", source.ID)
		resourceID := deterministicID("zqr_", source.ID)
		versionID := deterministicID("zqv_", source.ID)
		representationID := deterministicID("zqp_", source.ID)
		objectKey := physicalCOSObjectKey(source, m.cos)
		publicURL := physicalCOSPublicURL(source, objectKey, m.cos)
		resourceStatus := model.ResourceStatusReady
		resourceError := ""
		if objectKey == "" && publicURL == "" {
			resourceStatus = model.ResourceStatusFailed
			resourceError = "ZQ 素材缺少对象路径和 URL"
		}
		assetStatus := model.AssetVersionStatusConfirmed
		if source.IsDeleted {
			assetStatus = model.AssetVersionStatusArchived
			resourceStatus = model.ResourceStatusDeleted
		}
		targetKind := targetAssetKind(source)
		payload, err := sourceAssetPayload(source, lineage, assetID, versionID, resourceID, targetKind, assetStatus)
		if err != nil {
			return fmt.Errorf("构造 ZQ 素材 %s 载荷：%w", source.ID, err)
		}
		definition, _ := json.Marshal(map[string]any{"resourceId": resourceID, "url": publicURL, "kind": targetKind, "sourceSystem": sourceSystem})
		metadata, _ := json.Marshal(map[string]any{"sourceAssetId": source.ID, "visibility": source.Visibility, "objectKey": objectKey})
		err = m.target.Transaction(func(tx *gorm.DB) error {
			resource := model.Resource{ID: resourceID, UserID: userMap.TargetID, Kind: targetKind, Status: resourceStatus, Provider: "tencent", SourceSystem: sourceSystem, DeletionPolicy: "retain_shared", Endpoint: m.cos.publicDomain(), Bucket: m.cos.Bucket, ObjectKey: objectKey, PublicURL: publicURL, MimeType: source.MimeType, Width: source.Width, Height: source.Height, Error: resourceError, CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"user_id", "kind", "status", "provider", "source_system", "deletion_policy", "endpoint", "bucket", "object_key", "public_url", "mime_type", "width", "height", "error", "updated_at"})}).Create(&resource).Error; err != nil {
				return err
			}
			asset := model.Asset{ID: assetID, UserID: userMap.TargetID, Kind: targetKind, Category: model.AssetCategoryOther, Status: assetStatus, PrimaryVersionID: versionID, Title: source.Title, PayloadJSON: string(payload), CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"user_id", "kind", "status", "primary_version_id", "title", "payload_json", "updated_at"})}).Create(&asset).Error; err != nil {
				return err
			}
			version := model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: assetStatus, DefinitionJSON: string(definition), Note: "从 ZQ 素材库迁移", CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "definition_json", "updated_at"})}).Create(&version).Error; err != nil {
				return err
			}
			representation := model.AssetRepresentation{ID: representationID, AssetVersionID: versionID, ResourceID: resourceID, MediaType: targetKind, Role: "original", MetadataJSON: string(metadata), CreatedAt: source.SysCreateDatetime}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"resource_id", "media_type", "metadata_json"})}).Create(&representation).Error
		})
		if err != nil {
			return fmt.Errorf("写入 ZQ 素材 %s：%w", source.ID, err)
		}
		status := model.MigrationEntityImported
		if source.IsDeleted {
			status = model.MigrationEntitySkipped
		}
		if err := m.saveMap("asset", source.ID, assetID, status, source.SysUpdateDatetime, digest, map[string]any{"resourceId": resourceID, "objectKey": objectKey}); err != nil {
			return err
		}
		updateStats(stats, status)
	}
	return nil
}

func sourceAssetMetadata(source SourceAsset, task SourceGenerationTask) map[string]any {
	metadata := make(map[string]any)
	_ = json.Unmarshal(validJSON(source.AssetMetadata), &metadata)
	metadata["source_system"] = sourceSystem
	metadata["source_asset_id"] = source.ID
	metadata["source_kind"] = source.Kind
	metadata["ordinal"] = source.Ordinal
	metadata["visibility"] = source.Visibility
	if source.PublishedAt != nil {
		metadata["published_at"] = source.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	if source.GenerationTaskID != "" {
		metadata["generation_task_id"] = source.GenerationTaskID
	}
	if task.BatchID != "" {
		metadata["batch_id"] = task.BatchID
		metadata["batch_start_ordinal"] = task.BatchStartOrdinal
	}
	return metadata
}

func targetAssetKind(source SourceAsset) string {
	switch strings.ToLower(strings.TrimSpace(source.Kind)) {
	case "video":
		return "video"
	case "audio", "audio_reference":
		return "audio"
	case "image", "reference":
		return "image"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source.MimeType)), "video/") {
		return "video"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source.MimeType)), "audio/") {
		return "audio"
	}
	return "image"
}

func sourceAssetPayload(source SourceAsset, task SourceGenerationTask, assetID string, versionID string, resourceID string, kind string, status model.AssetVersionStatus) ([]byte, error) {
	resourceURL := "/api/resources/" + resourceID + "/file"
	storageKey := "resource:" + resourceID
	data := map[string]any{
		"storageKey": storageKey,
		"bytes":      0,
		"mimeType":   source.MimeType,
	}
	coverURL := ""
	switch kind {
	case "video":
		data["url"] = resourceURL
		data["width"] = source.Width
		data["height"] = source.Height
	case "audio":
		data["url"] = resourceURL
	default:
		data["dataUrl"] = resourceURL
		data["width"] = source.Width
		data["height"] = source.Height
		coverURL = resourceURL
	}
	return json.Marshal(map[string]any{
		"id":               assetID,
		"kind":             kind,
		"title":            source.Title,
		"coverUrl":         coverURL,
		"tags":             []string{},
		"category":         model.AssetCategoryOther,
		"status":           status,
		"primaryVersionId": versionID,
		"source":           source.Source,
		"createdAt":        source.SysCreateDatetime.UTC().Format(time.RFC3339Nano),
		"updatedAt":        source.SysUpdateDatetime.UTC().Format(time.RFC3339Nano),
		"metadata":         sourceAssetMetadata(source, task),
		"data":             data,
	})
}

func validJSON(raw []byte) []byte {
	if json.Valid(raw) {
		return raw
	}
	return []byte("{}")
}

func (m *Migrator) processVoiceProfiles(since time.Time, stats *Stats) error {
	var rows []SourceVoiceProfile
	if err := incrementalQuery(m.source.Model(&SourceVoiceProfile{}), since).Order("sys_update_datetime, id").Find(&rows).Error; err != nil {
		return fmt.Errorf("读取 ZQ 音色：%w", err)
	}
	for _, source := range rows {
		digest := checksum(source)
		userMap, userErr := m.latestMap("account", source.AccountID)
		assetMap, assetErr := m.latestMap("asset", source.SampleAssetID)
		if userErr != nil || assetErr != nil || userMap.TargetID == "" || assetMap.TargetID == "" {
			if err := m.conflict("voice_profile", source.ID, "owner_or_sample_not_migrated", source.SysUpdateDatetime, digest, nil); err != nil {
				return err
			}
			updateStats(stats, model.MigrationEntityConflict)
			continue
		}
		resourceID := deterministicID("zqr_", source.SampleAssetID)
		metadata, _ := json.Marshal(map[string]any{"sourceSystem": sourceSystem, "sourceVoiceProfileId": source.ID, "description": source.Description, "visibility": source.Visibility, "trainingMode": source.TrainingMode, "usageCount": source.UsageCount, "publishedAt": source.PublishedAt, "errorMessage": source.ErrorMessage, "profileMetadata": json.RawMessage(validJSON(source.ProfileMetadata))})
		status := targetVoiceStatus(source)
		voice := model.VoiceProfile{ID: deterministicID("zqvoice_", source.ID), UserID: userMap.TargetID, Name: source.Name, Provider: "zq-studio", VoiceKey: source.ID, Language: source.Language, Timbre: source.Description, SampleResourceID: resourceID, ReferenceText: source.ReferenceText, MetadataJSON: string(metadata), Status: status, CreatedAt: source.SysCreateDatetime, UpdatedAt: source.SysUpdateDatetime}
		if err := m.target.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"user_id", "name", "language", "timbre", "sample_resource_id", "reference_text", "metadata_json", "status", "updated_at"})}).Create(&voice).Error; err != nil {
			return err
		}
		mapStatus := model.MigrationEntityImported
		if source.IsDeleted {
			mapStatus = model.MigrationEntitySkipped
		}
		if err := m.saveMap("voice_profile", source.ID, voice.ID, mapStatus, source.SysUpdateDatetime, digest, nil); err != nil {
			return err
		}
		updateStats(stats, mapStatus)
	}
	return nil
}

func targetVoiceStatus(source SourceVoiceProfile) string {
	if source.IsDeleted {
		return "archived"
	}
	switch strings.ToLower(strings.TrimSpace(source.Status)) {
	case "active", "ready", "succeeded", "completed", "published":
		return "active"
	case "failed", "error":
		return "failed"
	default:
		return "processing"
	}
}
