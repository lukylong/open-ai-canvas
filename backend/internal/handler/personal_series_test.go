package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPersonalSeriesHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.Asset{}, &model.PersonalAssetSeries{}, &model.PersonalAssetMembership{}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("token"))
	for _, row := range []any{&model.User{ID: "owner", Username: "owner", Status: model.UserStatusActive, Role: model.UserRoleUser}, &model.AuthSession{ID: "session", UserID: "owner", TokenHash: hex.EncodeToString(hash[:]), ExpiresAt: time.Now().Add(time.Hour)}, &model.Asset{ID: "image", UserID: "owner", Kind: "image", Status: model.AssetVersionStatusConfirmed}, &model.Asset{ID: "foreign", UserID: "other", Kind: "image", Status: model.AssetVersionStatusConfirmed}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	RegisterPersonalSeriesRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	call := func(method, path, body string, auth bool, want int) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, "/api/personal-series"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.Code, want, res.Body)
		}
		var envelope struct {
			Code int                        `json:"code"`
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if (want == 200) != (envelope.Code == 0) {
			t.Fatalf("wrong envelope: %s", res.Body)
		}
		return envelope.Data
	}
	call("GET", "", "", false, 401)
	call("POST", "", `{"name":"我的系列","parentId":""}`, false, 401)
	created := call("POST", "", `{"name":"我的系列","parentId":""}`, true, 200)
	var series model.PersonalAssetSeries
	if err := json.Unmarshal(created["series"], &series); err != nil {
		t.Fatal(err)
	}
	call("POST", "/members", `{"seriesId":"`+series.ID+`","assetIds":["image"]}`, true, 200)
	call("POST", "/members", `{"seriesId":"`+series.ID+`","assetIds":["foreign"]}`, true, 404)
	call("PUT", "/"+series.ID+"/cover", `{"assetId":"image"}`, true, 200)
	call("PATCH", "/"+series.ID, `{"name":"改名","parentId":""}`, true, 200)
	state := call("GET", "", "", true, 200)
	if !strings.Contains(string(state["series"]), "改名") || !strings.Contains(string(state["memberships"]), "image") {
		t.Fatalf("state lost after reload: %#v", state)
	}
	call("DELETE", "/"+series.ID, "", true, 200)
	var count int64
	if err := db.Model(&model.Asset{}).Where("id = ?", "image").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("deleting series removed original asset")
	}
}
