package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSharedManagementHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.SharedAssetSeries{}, &model.SharedAsset{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive, SharedLibraryEnabled: true}
	tokenHash := sha256.Sum256([]byte("test-token"))
	for _, row := range []any{
		&user,
		&model.AuthSession{ID: "session", UserID: user.ID, TokenHash: hex.EncodeToString(tokenHash[:]), ExpiresAt: time.Now().Add(time.Hour)},
		&model.SystemSetting{Key: "feature_availability", ValueJSON: `{"sharedLibraryEnabled":true}`},
		&model.SharedAssetSeries{ID: "source", Name: "Source", OwnerUserID: user.ID, Status: model.SharedAssetSeriesReady},
		&model.SharedAssetSeries{ID: "target", Name: "Target", OwnerUserID: user.ID, Status: model.SharedAssetSeriesReady},
		&model.SharedAssetSeries{ID: "foreign", Name: "Foreign", OwnerUserID: "someone-else", Status: model.SharedAssetSeriesReady},
		&model.SharedAsset{ID: "image", SeriesID: "source", Title: "Image", ResourceID: "resource", UploadItemID: "item", MimeType: "image/png", Version: 1, Status: model.SharedAssetReady},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(repository.New(db), t.TempDir())
	router := gin.New()
	RegisterSharedLibraryRoutes(router.Group("/api"), svc)
	call := func(method, path, body string, authenticated bool, want int) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, "/api/shared-library"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.test-token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, res.Code, want, res.Body)
		}
		var envelope struct {
			Code int                        `json:"code"`
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if (want == 200) != (envelope.Code == 0) {
			t.Fatalf("HTTP/business status mismatch: %s", res.Body)
		}
		return envelope.Data
	}
	call("POST", "/assets/image/move", `{"seriesId":"target"}`, false, 401)
	call("PUT", "/series/source/cover", `{"assetId":"image"}`, false, 401)
	call("POST", "/assets/image/move", `{}`, true, 400)
	call("PUT", "/series/source/cover", `{}`, true, 400)
	call("POST", "/assets/image/move", `{"seriesId":"foreign"}`, true, 403)
	call("PUT", "/series/foreign/cover", `{"assetId":"image"}`, true, 403)
	cover := call("PUT", "/series/source/cover", `{"assetId":"image"}`, true, 200)
	if !strings.Contains(string(cover["series"]), `"coverResourceId":"resource"`) {
		t.Fatalf("cover was not returned: %s", cover["series"])
	}
	moved := call("POST", "/assets/image/move", `{"seriesId":"target"}`, true, 200)
	var asset model.SharedAsset
	if err := json.Unmarshal(moved["asset"], &asset); err != nil || asset.SeriesID != "target" || asset.Version != 1 {
		t.Fatalf("move response = %s, err=%v", moved["asset"], err)
	}
	call("PUT", "/series/source/cover", `{"assetId":"image"}`, true, 400)
	call("PUT", "/series/target/cover", `{"assetId":"image"}`, true, 200)
	rows := call("GET", "/series", "", true, 200)
	var categories []model.SharedAssetSeries
	if err := json.Unmarshal(rows["series"], &categories); err != nil {
		t.Fatal(err)
	}
	for _, row := range categories {
		if row.ID == "source" && row.CoverResourceID != "" || row.ID == "target" && row.CoverResourceID != "resource" {
			t.Fatalf("persisted cover mismatch: %#v", row)
		}
	}
	if err := db.Model(&user).Update("shared_library_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	call("POST", "/assets/image/move", `{"seriesId":"source"}`, true, 403)
	call("PUT", "/series/target/cover", `{"assetId":"image"}`, true, 403)
}
