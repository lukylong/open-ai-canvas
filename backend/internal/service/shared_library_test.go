package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSharedLibraryPermissionAndAdminAudit(t *testing.T) {
	svc, db := newSharedLibraryTestService(t)
	admin := model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	member := model.User{ID: "member", Username: "member", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&[]model.User{admin, member}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireSharedLibraryAccess(&member); err == nil {
		t.Fatal("default entitlement allowed a regular user")
	}
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireSharedLibraryAccess(&admin); err != nil {
		t.Fatalf("admin implicit access: %v", err)
	}
	if err := svc.RequireSharedLibraryAccess(&member); err == nil {
		t.Fatal("regular user without entitlement was allowed")
	}
	updated, err := svc.UpdateUserSharedLibraryAccess(&admin, member.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.SharedLibraryEnabled {
		t.Fatal("entitlement was not saved")
	}
	if err := svc.RequireSharedLibraryAccess(updated); err != nil {
		t.Fatalf("entitled user rejected: %v", err)
	}
	var count int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ? AND target_id = ?", "shared_library.access.update", member.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit count = %d, want 1", count)
	}
}

func TestSharedUploadBoundaryAndIdempotentComplete(t *testing.T) {
	svc, db := newSharedLibraryTestService(t)
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	member := model.User{ID: "member", Username: "member", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	series, err := svc.CreateSharedAssetSeries(&member, "测试系列")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]SharedUploadManifestItem, sharedBatchMaxFiles+1)
	for i := range tooMany {
		tooMany[i] = SharedUploadManifestItem{ClientID: newID(), FileName: "x.png", Size: 1}
	}
	if _, err := svc.CreateSharedUploadBatch(&member, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: series.ID, Files: tooMany}); err == nil {
		t.Fatal("1001 files were accepted")
	}
	boundary := make([]SharedUploadManifestItem, sharedBatchMaxFiles)
	baseSize := sharedBatchMaxBytes / int64(sharedBatchMaxFiles)
	for i := range boundary {
		boundary[i] = SharedUploadManifestItem{ClientID: fmt.Sprintf("boundary-%d", i), FileName: fmt.Sprintf("%d.png", i), Size: baseSize}
	}
	boundary[len(boundary)-1].Size += sharedBatchMaxBytes - baseSize*int64(len(boundary))
	if _, err := svc.CreateSharedUploadBatch(&member, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: series.ID, Files: boundary}); err != nil {
		t.Fatalf("exact 1000-file/5GB boundary rejected: %v", err)
	}
	boundary[len(boundary)-1].Size++
	if _, err := svc.CreateSharedUploadBatch(&member, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: series.ID, Files: boundary}); err == nil {
		t.Fatal("5GB + 1 byte batch was accepted")
	}

	payload := testPNG(t)
	sum := sha256.Sum256(payload)
	detail, err := svc.CreateSharedUploadBatch(&member, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: series.ID, Files: []SharedUploadManifestItem{{ClientID: "png-1", FileName: "cover.png", MimeType: "image/png", Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}}})
	if err != nil {
		t.Fatal(err)
	}
	target := detail.Uploads[0]
	if _, err := svc.UploadSharedItemContent(&member, detail.Batch.ID, target.ItemID, target.Token, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	first, err := svc.CompleteSharedUploadItem(&member, detail.Batch.ID, target.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CompleteSharedUploadItem(&member, detail.Batch.ID, target.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].AssetID == "" || first.Items[0].AssetID != second.Items[0].AssetID {
		t.Fatalf("idempotent asset mismatch: %#v %#v", first.Items[0], second.Items[0])
	}
	var count int64
	if err := db.Model(&model.SharedAsset{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("asset count = %d, want 1", count)
	}
	sharedAsset, err := svc.repo.SharedAsset(first.Items[0].AssetID)
	if err != nil {
		t.Fatal(err)
	}
	media := providerMedia{AssetReference: &providerAssetReference{Source: "shared", SharedAssetID: sharedAsset.ID, Version: sharedAsset.Version}}
	if err := svc.hydrateProviderMedia(member.ID, &media, false); err != nil {
		t.Fatalf("hydrate shared provider media: %v", err)
	}
	if !strings.HasPrefix(media.DataURL, "data:image/png;base64,") || media.StorageKey != "resource:"+sharedAsset.ResourceID {
		t.Fatalf("hydrated shared media = %#v", media)
	}
	invalidVersion := map[string]any{"referenceImages": []any{map[string]any{"assetReference": map[string]any{"source": "shared", "sharedAssetId": sharedAsset.ID, "version": float64(sharedAsset.Version + 1)}}}}
	if err := svc.ValidateSharedAssetReferences(member.ID, invalidVersion); err == nil {
		t.Fatal("stale shared asset version was accepted")
	}
	member.SharedLibraryEnabled = false
	if err := db.Save(&member).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareSharedAssetDelivery(&member, first.Items[0].AssetID, false, ""); err == nil {
		t.Fatal("revoked user retained file access")
	}
	if err := svc.hydrateProviderMedia(member.ID, &providerMedia{AssetReference: &providerAssetReference{Source: "shared", SharedAssetID: sharedAsset.ID, Version: sharedAsset.Version}}, false); err == nil {
		t.Fatal("revoked user retained generation reference access")
	}
}

func TestSharedSeriesOwnershipAndZIPEntrySecurity(t *testing.T) {
	svc, db := newSharedLibraryTestService(t)
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	owner := model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	other := model.User{ID: "other", Username: "other", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	if err := db.Create(&[]model.User{owner, other}).Error; err != nil {
		t.Fatal(err)
	}
	series, err := svc.CreateSharedAssetSeries(&owner, "owner series")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSharedUploadBatch(&other, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: series.ID, Files: []SharedUploadManifestItem{{ClientID: "other", FileName: "other.png", Size: 1}}})
	if err == nil {
		t.Fatal("another user uploaded into the owner's series")
	}

	for _, entry := range []*zip.File{
		{FileHeader: zip.FileHeader{Name: "encrypted.png", Flags: 1}},
		{FileHeader: zip.FileHeader{Name: "../escape.png"}},
		{FileHeader: zip.FileHeader{Name: `C:\\escape.png`}},
		func() *zip.File {
			header := zip.FileHeader{Name: "link.png"}
			header.SetMode(os.ModeSymlink | 0o644)
			return &zip.File{FileHeader: header}
		}(),
	} {
		if err := validateSharedZIPEntry(entry); err == nil {
			t.Fatalf("unsafe ZIP entry accepted: %#v", entry.FileHeader)
		}
	}
}

func TestSharedZIPLeaseCanBeReclaimedWithoutRedis(t *testing.T) {
	svc, db := newSharedLibraryTestService(t)
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	member := model.User{ID: "member", Username: "member", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	detail := uploadSharedZIPForTest(t, svc, &member, testZIP(t, map[string][]byte{"good.png": testPNG(t)}), "lease.zip")
	first, err := svc.repo.ClaimNextSharedZIPBatch("worker-one", time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first claim: %v %#v", err, first)
	}
	if err := db.Model(&model.SharedAssetUploadBatch{}).Where("id = ?", detail.Batch.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.repo.ClaimNextSharedZIPBatch("worker-two", time.Minute)
	if err != nil || second == nil || second.ID != detail.Batch.ID {
		t.Fatalf("reclaim: %v %#v", err, second)
	}
}

func TestSharedZIPPartialImportAndUnsafePath(t *testing.T) {
	svc, db := newSharedLibraryTestService(t)
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	member := model.User{ID: "member", Username: "member", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	payload := testZIP(t, map[string][]byte{"folder/good.png": testPNG(t), "folder/bad.jpg": []byte("broken"), "__MACOSX/.meta": []byte("ignored")})
	detail := uploadSharedZIPForTest(t, svc, &member, payload, "partial.zip")
	claimed, err := svc.repo.ClaimNextSharedZIPBatch(svc.workerID, sharedUploadLease)
	if err != nil {
		t.Fatal(err)
	}
	svc.processSharedZIPBatch(claimed)
	finished, err := svc.SharedUploadBatchDetail(&member, detail.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Batch.Status != model.SharedBatchCompletedWithErrors || finished.Batch.ReadyCount != 1 || finished.Batch.SkippedCount != 1 {
		t.Fatalf("partial ZIP result = %#v", finished.Batch)
	}
	assets, err := svc.SharedAssets(&member, finished.Batch.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Title != "good" {
		t.Fatalf("assets = %#v", assets)
	}

	unsafe := testZIP(t, map[string][]byte{"../escape.png": testPNG(t)})
	unsafeDetail := uploadSharedZIPForTest(t, svc, &member, unsafe, "unsafe.zip")
	claimed, err = svc.repo.ClaimNextSharedZIPBatch(svc.workerID, sharedUploadLease)
	if err != nil {
		t.Fatal(err)
	}
	svc.processSharedZIPBatch(claimed)
	failed, err := svc.SharedUploadBatchDetail(&member, unsafeDetail.Batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Batch.Status != model.SharedBatchFailed {
		t.Fatalf("unsafe ZIP status = %s", failed.Batch.Status)
	}
}

func newSharedLibraryTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.SystemSetting{}, &model.AdminAuditEvent{}, &model.StorageLocation{}, &model.UserOSSSetting{}, &model.UserDailyUploadUsage{}, &model.Resource{}, &model.SharedAssetSeries{}, &model.SharedAsset{}, &model.SharedAssetUploadBatch{}, &model.SharedAssetUploadItem{}, &model.ProjectSharedAssetLink{}); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir()), db
}

func enableSharedLibraryFeature(db *gorm.DB) error {
	value := defaultFeatureAvailability()
	value.SharedLibraryEnabled = true
	return db.Create(&model.SystemSetting{Key: featureAvailabilitySettingKey, ValueJSON: sharedJSON(value), UpdatedBy: "admin", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	var payload bytes.Buffer
	if err := png.Encode(&payload, imageData); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}

func testZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}

func uploadSharedZIPForTest(t *testing.T, svc *Service, member *model.User, payload []byte, name string) *SharedUploadBatchDetail {
	t.Helper()
	sum := sha256.Sum256(payload)
	detail, err := svc.CreateSharedUploadBatch(member, CreateSharedUploadBatchRequest{Mode: "zip", SeriesName: name, Files: []SharedUploadManifestItem{{ClientID: name, FileName: name, MimeType: "application/zip", Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}}})
	if err != nil {
		t.Fatal(err)
	}
	target := detail.Uploads[0]
	if _, err := svc.UploadSharedItemContent(member, detail.Batch.ID, target.ItemID, target.Token, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.CompleteSharedUploadItem(member, detail.Batch.ID, target.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}
