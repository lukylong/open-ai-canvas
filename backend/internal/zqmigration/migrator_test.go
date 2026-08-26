package zqmigration

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVerifyMatchesActiveSourceIDsInsteadOfOnlyComparingTotals(t *testing.T) {
	source, err := gorm.Open(sqlite.Open("file:zq-verify-source?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := gorm.Open(sqlite.Open("file:zq-verify-target?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.AutoMigrate(&SourceAccount{}, &SourceAsset{}, &SourceVoiceProfile{}, &SourceInvitationCode{}, &SourceInvitationUsage{}); err != nil {
		t.Fatal(err)
	}
	if err := target.AutoMigrate(&model.MigrationEntityMap{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	active := SourceAccount{SourceBase: SourceBase{ID: "active", SysCreateDatetime: now, SysUpdateDatetime: now}, Username: "active"}
	deleted := SourceAccount{SourceBase: SourceBase{ID: "deleted", IsDeleted: true, SysCreateDatetime: now, SysUpdateDatetime: now}, Username: "deleted"}
	if err := source.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	deletedMap := model.MigrationEntityMap{ID: "map-deleted", SourceSystem: sourceSystem, EntityType: "account", SourceID: deleted.ID, TargetID: "target-deleted", Status: model.MigrationEntityImported, CreatedAt: now, UpdatedAt: now}
	if err := target.Create(&deletedMap).Error; err != nil {
		t.Fatal(err)
	}

	verification, err := New(source, target, t.TempDir(), COSConfig{}).Verify()
	if err != nil {
		t.Fatal(err)
	}
	if verification.Mapped["account"] != 0 || verification.Missing["account"] != 1 {
		t.Fatalf("account verification = mapped %d missing %d", verification.Mapped["account"], verification.Missing["account"])
	}
}

func TestBackfillPreservesAssetBatchLineage(t *testing.T) {
	source, err := gorm.Open(sqlite.Open("file:zq-lineage-source?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := gorm.Open(sqlite.Open("file:zq-lineage-target?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.AutoMigrate(&SourceAccount{}, &SourceAsset{}, &SourceGenerationTask{}, &SourceVoiceProfile{}, &SourceInvitationCode{}, &SourceInvitationUsage{}); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(target); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	account := SourceAccount{SourceBase: SourceBase{ID: "account-lineage", SysCreateDatetime: now, SysUpdateDatetime: now}, Username: "lineage-user", PasswordHash: "$2b$12$preserved", Status: "active"}
	task := SourceGenerationTask{ID: "task-lineage", BatchID: "batch-lineage", BatchStartOrdinal: 200}
	asset := SourceAsset{
		SourceBase:       SourceBase{ID: "asset-lineage", SysCreateDatetime: now, SysUpdateDatetime: now},
		AccountID:        account.ID,
		GenerationTaskID: task.ID,
		Kind:             "video",
		Title:            "Lineage Video",
		URL:              "https://example.com/lineage.mp4",
		MimeType:         "video/mp4",
		Ordinal:          205,
		AssetMetadata:    []byte(`{"batch_index":5,"batch_size":20}`),
	}
	for _, value := range []any{&account, &task, &asset} {
		if err := source.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(source, target, t.TempDir(), COSConfig{}).Backfill(); err != nil {
		t.Fatal(err)
	}
	var migrated model.Asset
	if err := target.First(&migrated, "title = ?", asset.Title).Error; err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ID               string         `json:"id"`
		Kind             string         `json:"kind"`
		Status           string         `json:"status"`
		PrimaryVersionID string         `json:"primaryVersionId"`
		CreatedAt        string         `json:"createdAt"`
		UpdatedAt        string         `json:"updatedAt"`
		Metadata         map[string]any `json:"metadata"`
		Data             map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(migrated.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Metadata["generation_task_id"] != task.ID || payload.Metadata["batch_id"] != task.BatchID || payload.Metadata["batch_start_ordinal"] != float64(task.BatchStartOrdinal) {
		t.Fatalf("lineage metadata = %#v", payload.Metadata)
	}
	if payload.ID != migrated.ID || payload.Kind != "video" || payload.Status != string(model.AssetVersionStatusConfirmed) || payload.PrimaryVersionID != migrated.PrimaryVersionID {
		t.Fatalf("frontend asset identity = %#v", payload)
	}
	if payload.CreatedAt == "" || payload.UpdatedAt == "" || payload.Data["storageKey"] != "resource:"+deterministicID("zqr_", asset.ID) || payload.Data["url"] != "/api/resources/"+deterministicID("zqr_", asset.ID)+"/file" {
		t.Fatalf("frontend asset data = %#v", payload)
	}
	if payload.Metadata["ordinal"] != float64(asset.Ordinal) || payload.Metadata["source_kind"] != asset.Kind {
		t.Fatalf("frontend asset ordering metadata = %#v", payload.Metadata)
	}
}

func TestTargetAssetKindMapsZQReferenceKindsToFrontendMediaKinds(t *testing.T) {
	tests := []struct {
		kind string
		mime string
		want string
	}{
		{kind: "image", want: "image"},
		{kind: "reference", want: "image"},
		{kind: "video", want: "video"},
		{kind: "audio_reference", want: "audio"},
		{kind: "legacy", mime: "audio/mpeg", want: "audio"},
	}
	for _, test := range tests {
		if got := targetAssetKind(SourceAsset{Kind: test.kind, MimeType: test.mime}); got != test.want {
			t.Fatalf("targetAssetKind(%q, %q) = %q, want %q", test.kind, test.mime, got, test.want)
		}
	}
}

func TestBackfillPreservesBcryptAccountAndCreatesIdempotentMap(t *testing.T) {
	source, err := gorm.Open(sqlite.Open("file:zq-backfill-source?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := gorm.Open(sqlite.Open("file:zq-backfill-target?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.AutoMigrate(&SourceAccount{}, &SourceAsset{}, &SourceVoiceProfile{}, &SourceInvitationCode{}, &SourceInvitationUsage{}); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(target); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	account := SourceAccount{SourceBase: SourceBase{ID: "account-1", SysCreateDatetime: now, SysUpdateDatetime: now}, Username: "zq_user", Email: "zq@example.com", PasswordHash: "$2b$12$preserved", DisplayName: "ZQ 用户", Status: "active"}
	if err := source.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	migrator := New(source, target, t.TempDir(), COSConfig{})
	if _, err := migrator.Backfill(); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Backfill(); err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := target.First(&user, "username = ?", account.Username).Error; err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash != account.PasswordHash || user.SourceSystem != sourceSystem {
		t.Fatalf("migrated user = %#v", user)
	}
	var count int64
	if err := target.Model(&model.MigrationEntityMap{}).Where("source_system = ? AND entity_type = ? AND source_id = ?", sourceSystem, "account", account.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("mapping count = %d, want 1", count)
	}
}
