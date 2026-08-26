package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestManualDistributionPublicationDispatchesSignedOutbox(t *testing.T) {
	const secret = "distribution-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		canonical := request.Header.Get("X-Asset-Sync-Timestamp") + "\n" + request.Header.Get("X-Asset-Sync-Nonce") + "\n" + string(body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		if request.Header.Get("X-Asset-Sync-Key") != "canvas-key" || !hmac.Equal([]byte(request.Header.Get("X-Asset-Sync-Signature")), []byte(hex.EncodeToString(mac.Sum(nil)))) {
			http.Error(writer, "invalid signature", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1,"data":{"error":0,"results":[]}}`))
	}))
	defer server.Close()
	t.Setenv("CANVAS_DISTRIBUTION_URL", server.URL)
	t.Setenv("CANVAS_DISTRIBUTION_KEY_ID", "canvas-key")
	t.Setenv("CANVAS_DISTRIBUTION_SECRET", secret)

	db, err := gorm.Open(sqlite.Open("file:distribution?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Resource{}, &model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.DistributionPublication{}, &model.DistributionOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	user := &model.User{ID: "user-1", Username: "user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	resource := &model.Resource{ID: "resource-1", UserID: user.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "tencent", Bucket: "bucket", ObjectKey: "canvas/assets/image.png", PublicURL: "https://example.com/image.png"}
	asset := &model.Asset{ID: "asset-1", UserID: user.ID, Kind: "image", Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: "version-1", Title: "Image", UpdatedAt: now}
	version := &model.AssetVersion{ID: "version-1", AssetID: asset.ID, Version: 1, Status: model.AssetVersionStatusConfirmed}
	representation := &model.AssetRepresentation{ID: "representation-1", AssetVersionID: version.ID, ResourceID: resource.ID, MediaType: "image", Role: "original"}
	for _, value := range []any{user, resource, asset, version, representation} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := New(repository.New(db), t.TempDir())
	var before int64
	if err := db.Model(&model.DistributionPublication{}).Count(&before).Error; err != nil || before != 0 {
		t.Fatalf("automatic publication count = %d, err = %v", before, err)
	}
	publication, err := svc.CreateDistributionPublication(user, asset.ID, CreateDistributionPublicationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if publication.Status != model.DistributionPublicationPending {
		t.Fatalf("status = %q", publication.Status)
	}
	duplicate, err := svc.CreateDistributionPublication(user, asset.ID, CreateDistributionPublicationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != publication.ID {
		t.Fatalf("duplicate publication = %s, want %s", duplicate.ID, publication.ID)
	}
	if err := svc.DispatchDistributionOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(publication, "id = ?", publication.ID).Error; err != nil {
		t.Fatal(err)
	}
	if publication.Status != model.DistributionPublicationPublished || publication.PublishedAt == nil {
		t.Fatalf("publication = %#v", publication)
	}
}

func TestBatchDistributionAcceptsPayloadBackedCanvasAssets(t *testing.T) {
	t.Setenv("CANVAS_DISTRIBUTION_URL", "https://distribution.example.test/api/resources/sync")
	t.Setenv("CANVAS_DISTRIBUTION_KEY_ID", "canvas-key")
	t.Setenv("CANVAS_DISTRIBUTION_SECRET", "distribution-secret")

	db, err := gorm.Open(sqlite.Open("file:distribution-batch?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Resource{}, &model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.DistributionPublication{}, &model.DistributionOutbox{}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-batch", Username: "batch-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	resource := &model.Resource{ID: "resource-canvas", UserID: user.ID, Kind: "video", Status: model.ResourceStatusReady, Provider: "tencent", Endpoint: "https://cos.ap-guangzhou.myqcloud.com", Bucket: "bucket-1250000000", ObjectKey: "canvas/assets/video demo.mp4", MimeType: "video/mp4"}
	asset := &model.Asset{ID: "asset-canvas", UserID: user.ID, Kind: "video", Status: model.AssetVersionStatusConfirmed, Title: "Canvas Video", PayloadJSON: `{"data":{"storageKey":"resource:resource-canvas"}}`, UpdatedAt: time.Now()}
	for _, value := range []any{user, resource, asset} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := New(repository.New(db), t.TempDir())
	result, err := svc.CreateDistributionPublications(user, CreateDistributionPublicationsRequest{
		AssetIDs: []string{asset.ID, asset.ID, "missing-asset"},
		Metadata: map[string]any{"series_id": "batch-1", "series_type": "batch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestedCount != 2 || result.AcceptedCount != 1 || result.FailedCount != 1 {
		t.Fatalf("batch result = %#v", result)
	}
	if result.Items[0].Publication == nil || !strings.HasPrefix(result.Items[0].Publication.AssetVersionID, "payload-") {
		t.Fatalf("payload publication = %#v", result.Items[0])
	}
	var event distributionEvent
	if err := json.Unmarshal([]byte(result.Items[0].Publication.PayloadJSON), &event); err != nil {
		t.Fatal(err)
	}
	if event.Source != "zq-media-studio" || event.Resources[0].Metadata["series_id"] != "batch-1" {
		t.Fatalf("distribution event = %#v", event)
	}
	if event.Resources[0].FileURL != "https://bucket-1250000000.cos.ap-guangzhou.myqcloud.com/canvas/assets/video%20demo.mp4" || event.Resources[0].FilePath != resource.ObjectKey {
		t.Fatalf("private COS distribution resource = %#v", event.Resources[0])
	}
	var publications int64
	if err := db.Model(&model.DistributionPublication{}).Count(&publications).Error; err != nil || publications != 1 {
		t.Fatalf("publication count = %d, err = %v", publications, err)
	}
	repeated, err := svc.CreateDistributionPublications(user, CreateDistributionPublicationsRequest{AssetIDs: []string{asset.ID}})
	if err != nil || repeated.AcceptedCount != 1 || repeated.Items[0].Publication.ID != result.Items[0].Publication.ID {
		t.Fatalf("repeated batch = %#v, err = %v", repeated, err)
	}
	tooMany := make([]string, MaxDistributionBatchAssets+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("asset-%04d", index)
	}
	if _, err := svc.CreateDistributionPublications(user, CreateDistributionPublicationsRequest{AssetIDs: tooMany}); err == nil {
		t.Fatal("expected batch size validation error")
	}
	lineage := distributionPayloadLineage(`{"metadata":{"batchId":"canvas-batch","taskId":"canvas-task","batchIndex":3}}`, "Canvas Batch · 4/10")
	if lineage["series_id"] != "canvas-batch" || lineage["batch_id"] != "canvas-batch" || lineage["batch_index"] != float64(3) {
		t.Fatalf("canvas lineage = %#v", lineage)
	}
}
