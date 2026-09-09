package service

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type sharedManagementFixture struct {
	svc                 *Service
	db                  *gorm.DB
	owner, other, admin model.User
	root, child, target *model.SharedAssetSeries
	foreign             *model.SharedAssetSeries
	asset               model.SharedAsset
}

func newSharedManagementFixture(t *testing.T) sharedManagementFixture {
	t.Helper()
	svc, db := newSharedLibraryTestService(t)
	if err := enableSharedLibraryFeature(db); err != nil {
		t.Fatal(err)
	}
	f := sharedManagementFixture{svc: svc, db: db,
		owner: model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true},
		other: model.User{ID: "other", Username: "other", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true},
		admin: model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive},
	}
	if err := db.Create(&[]model.User{f.owner, f.other, f.admin}).Error; err != nil {
		t.Fatal(err)
	}
	create := func(user *model.User, name, parent string) *model.SharedAssetSeries {
		row, err := svc.CreateSharedAssetSeries(user, name, parent)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	f.root = create(&f.owner, "一级分类", "")
	f.child = create(&f.owner, "子分类", f.root.ID)
	f.target = create(&f.owner, "其他分类", "")
	f.foreign = create(&f.other, "其他账号分类", "")
	f.asset = model.SharedAsset{ID: "image", SeriesID: f.child.ID, UploaderUserID: f.owner.ID, ResourceID: "resource", ThumbnailResourceID: "thumbnail", UploadItemID: "upload-item", Title: "图片", MimeType: "image/png", SHA256: "sha", Size: 123, Width: 2, Height: 2, Version: 3, Status: model.SharedAssetReady}
	if err := db.Create(&f.asset).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSharedAssetMovePreservesReferencesAndClearsOutsideCovers(t *testing.T) {
	f := newSharedManagementFixture(t)
	for _, series := range []*model.SharedAssetSeries{f.root, f.child} {
		if _, err := f.svc.SetSharedAssetSeriesCover(&f.owner, series.ID, f.asset.ID); err != nil {
			t.Fatal(err)
		}
	}
	link := model.ProjectSharedAssetLink{ID: "link", ProjectID: "project", SharedAssetID: f.asset.ID, Version: f.asset.Version, CreatedBy: f.owner.ID}
	if err := f.db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	moved, err := f.svc.MoveSharedAsset(&f.owner, f.asset.ID, f.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.SeriesID != f.target.ID || moved.ID != f.asset.ID || moved.Version != f.asset.Version || moved.ResourceID != f.asset.ResourceID || moved.ThumbnailResourceID != f.asset.ThumbnailResourceID || moved.Title != f.asset.Title || moved.UploaderUserID != f.asset.UploaderUserID || moved.SHA256 != f.asset.SHA256 {
		t.Fatalf("move changed identity/content: %#v", moved)
	}
	for _, series := range []*model.SharedAssetSeries{f.root, f.child} {
		row, err := f.svc.repo.SharedAssetSeries(series.ID)
		if err != nil || row.CoverResourceID != "" {
			t.Fatalf("stale cover: %#v, %v", row, err)
		}
	}
	for id, count := range map[string]int{f.child.ID: 0, f.target.ID: 1} {
		rows, err := f.svc.SharedAssets(&f.owner, id)
		if err != nil || len(rows) != count {
			t.Fatalf("series %s assets = %d, err=%v", id, len(rows), err)
		}
	}
	var persisted model.ProjectSharedAssetLink
	if err := f.db.First(&persisted, "id = ?", link.ID).Error; err != nil || persisted.Version != link.Version || persisted.SharedAssetID != link.SharedAssetID {
		t.Fatalf("project link changed: %#v, %v", persisted, err)
	}
	reference := map[string]any{"source": "shared", "sharedAssetId": f.asset.ID, "version": f.asset.Version}
	if err := f.svc.ValidateSharedAssetReferences(f.owner.ID, reference); err != nil {
		t.Fatalf("existing generation reference failed after move: %v", err)
	}
	repeated, err := f.svc.MoveSharedAsset(&f.owner, f.asset.ID, f.target.ID)
	if err != nil || !repeated.UpdatedAt.Equal(moved.UpdatedAt) {
		t.Fatalf("same-target move not idempotent: %#v, %v", repeated, err)
	}
}

func TestSharedAssetMoveWithinTreeRetainsAncestorCover(t *testing.T) {
	f := newSharedManagementFixture(t)
	if _, err := f.svc.SetSharedAssetSeriesCover(&f.owner, f.root.ID, f.asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MoveSharedAsset(&f.owner, f.asset.ID, f.root.ID); err != nil {
		t.Fatal(err)
	}
	root, err := f.svc.repo.SharedAssetSeries(f.root.ID)
	if err != nil || root.CoverResourceID != f.asset.ResourceID {
		t.Fatalf("ancestor cover lost: %#v, %v", root, err)
	}
}

func TestSharedAssetMoveAndCoverPermissionValidation(t *testing.T) {
	for _, name := range []string{"source-owner", "target-owner", "empty-target", "missing-target", "archived-target", "archived-source", "archived-asset", "revoked", "disabled", "cover-owner", "cover-outside-tree", "cover-missing-asset", "cover-archived-asset", "cover-archived-category", "cover-revoked", "cover-not-image"} {
		t.Run(name, func(t *testing.T) {
			f := newSharedManagementFixture(t)
			actor, target, assetID, coverID := &f.owner, f.target.ID, f.asset.ID, f.root.ID
			status := http.StatusBadRequest
			isCover := false
			set := func(row any, field string, value any) {
				if err := f.db.Model(row).Update(field, value).Error; err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "source-owner":
				actor, status = &f.other, http.StatusForbidden
			case "target-owner":
				target, status = f.foreign.ID, http.StatusForbidden
			case "empty-target":
				target = "  "
			case "missing-target":
				target = "missing"
			case "archived-target":
				set(f.target, "status", model.SharedAssetSeriesArchived)
			case "archived-source":
				set(f.child, "status", model.SharedAssetSeriesArchived)
				status = http.StatusNotFound
			case "archived-asset":
				set(&f.asset, "status", model.SharedAssetArchived)
				status = http.StatusNotFound
			case "revoked":
				f.owner.SharedLibraryEnabled, status = false, http.StatusForbidden
			case "disabled":
				f.owner.Status, status = model.UserStatus("disabled"), http.StatusForbidden
			case "cover-owner":
				isCover, actor, status = true, &f.other, http.StatusForbidden
			case "cover-outside-tree":
				isCover, coverID = true, f.target.ID
			case "cover-missing-asset":
				isCover, assetID = true, "missing"
			case "cover-archived-asset":
				isCover = true
				set(&f.asset, "status", model.SharedAssetArchived)
			case "cover-archived-category":
				isCover = true
				set(f.child, "status", model.SharedAssetSeriesArchived)
			case "cover-revoked":
				isCover, f.owner.SharedLibraryEnabled, status = true, false, http.StatusForbidden
			case "cover-not-image":
				isCover = true
				set(&f.asset, "mime_type", "video/mp4")
			}
			var err error
			if isCover {
				_, err = f.svc.SetSharedAssetSeriesCover(actor, coverID, assetID)
			} else {
				_, err = f.svc.MoveSharedAsset(actor, assetID, target)
			}
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != status {
				t.Fatalf("error = %v, want HTTP %d", err, status)
			}
			asset, _ := f.svc.repo.SharedAsset(f.asset.ID)
			root, _ := f.svc.repo.SharedAssetSeries(f.root.ID)
			if asset.SeriesID != f.child.ID || asset.Version != f.asset.Version || root.CoverResourceID != "" {
				t.Fatal("rejected write changed asset or cover")
			}
		})
	}
}

func TestSharedSeriesCoverPersistsAndArchiveClearsIt(t *testing.T) {
	f := newSharedManagementFixture(t)
	chosen, err := f.svc.SetSharedAssetSeriesCover(&f.owner, f.root.ID, f.asset.ID)
	if err != nil || chosen.CoverResourceID != f.asset.ResourceID {
		t.Fatalf("cover save: %#v, %v", chosen, err)
	}
	if _, err := f.svc.UpdateSharedAsset(&f.owner, f.asset.ID, "新标题"); err != nil {
		t.Fatal(err)
	}
	rows, err := f.svc.SharedAssetSeriesList(&f.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == f.root.ID && (row.CoverResourceID != f.asset.ResourceID || row.Name != f.root.Name || row.ParentID != f.root.ParentID) {
			t.Fatalf("cover did not survive fresh read: %#v", row)
		}
	}
	if err := f.svc.DeleteSharedAsset(&f.owner, f.asset.ID); err != nil {
		t.Fatal(err)
	}
	root, _ := f.svc.repo.SharedAssetSeries(f.root.ID)
	if root.CoverResourceID != "" {
		t.Fatal("archiving cover image left a stale cover")
	}
}

func TestSharedAssetMoveAdminAndTransactionRollback(t *testing.T) {
	t.Run("admin-cross-owner", func(t *testing.T) {
		f := newSharedManagementFixture(t)
		if _, err := f.svc.MoveSharedAsset(&f.admin, f.asset.ID, f.foreign.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.SetSharedAssetSeriesCover(&f.admin, f.foreign.ID, f.asset.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cover-failure-rolls-back-move", func(t *testing.T) {
		f := newSharedManagementFixture(t)
		if _, err := f.svc.SetSharedAssetSeriesCover(&f.owner, f.child.ID, f.asset.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Exec("CREATE TRIGGER fail_cover BEFORE UPDATE OF cover_resource_id ON shared_asset_series BEGIN SELECT RAISE(ABORT, 'injected cover failure'); END").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.MoveSharedAsset(&f.owner, f.asset.ID, f.target.ID); err == nil {
			t.Fatal("injected failure was swallowed")
		}
		asset, _ := f.svc.repo.SharedAsset(f.asset.ID)
		child, _ := f.svc.repo.SharedAssetSeries(f.child.ID)
		if asset.SeriesID != f.child.ID || child.CoverResourceID != f.asset.ResourceID {
			t.Fatal("transaction did not restore asset and cover")
		}
	})
}

func TestSharedAssetMoveKeepsUploadedImageReadable(t *testing.T) {
	f := newSharedManagementFixture(t)
	payload := testPNG(t)
	detail, err := f.svc.CreateSharedUploadBatch(&f.owner, CreateSharedUploadBatchRequest{Mode: "files", SeriesID: f.child.ID, Files: []SharedUploadManifestItem{{ClientID: "real-image", FileName: "real.png", MimeType: "image/png", Size: int64(len(payload))}}})
	if err != nil {
		t.Fatal(err)
	}
	upload := detail.Uploads[0]
	if _, err := f.svc.UploadSharedItemContent(&f.owner, detail.Batch.ID, upload.ItemID, upload.Token, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	complete, err := f.svc.CompleteSharedUploadItem(&f.owner, detail.Batch.ID, upload.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := f.svc.MoveSharedAsset(&f.owner, complete.Items[0].AssetID, f.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	media := providerMedia{AssetReference: &providerAssetReference{Source: "shared", SharedAssetID: moved.ID, Version: 1}}
	if err := f.svc.hydrateProviderMedia(f.owner.ID, &media, false); err != nil || media.DataURL == "" {
		t.Fatalf("moved image generation delivery: %v", err)
	}
	if _, err := f.svc.PrepareSharedAssetDelivery(&f.owner, moved.ID, true, ""); err != nil {
		t.Fatalf("moved thumbnail delivery: %v", err)
	}
}
