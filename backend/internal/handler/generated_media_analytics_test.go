package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestGeneratedMediaAnalyticsDeniesMissingAndIncorrectMachineCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CANVAS_ANALYTICS_READ_TOKEN", "test-read-only-secret-with-at-least-32-characters")
	r := gin.New()
	RegisterGeneratedMediaAnalyticsRoutes(r.Group("/api"), nil)
	for _, header := range []string{"", "Bearer incorrect-read-only-secret-with-32-characters", "Basic test-read-only-secret-with-at-least-32-characters"} {
		request := httptest.NewRequest(http.MethodGet, "/api/internal/v1/analytics/materials", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("header %q returned %d, want 401", header, response.Code)
		}
	}
	t.Setenv("CANVAS_ANALYTICS_READ_TOKEN", "")
	request := httptest.NewRequest(http.MethodGet, "/api/internal/v1/analytics/materials", nil)
	request.Header.Set("Authorization", "Bearer test-read-only-secret-with-at-least-32-characters")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured endpoint returned %d, want 401", response.Code)
	}
}

func TestGeneratedMediaAnalyticsReturnsOnlyAuthorizedAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CANVAS_ANALYTICS_READ_TOKEN", "test-read-only-secret-with-at-least-32-characters")
	db, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:material-auth?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.GeneratedMediaOutput{TaskID: "task", ResourceID: "resource", UserID: "private-user", Kind: "image", Model: "image-model", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterGeneratedMediaAnalyticsRoutes(r.Group("/api"), service.New(repository.New(db), t.TempDir()))
	request := httptest.NewRequest(http.MethodGet, "/api/internal/v1/analytics/materials", nil)
	request.Header.Set("Authorization", "Bearer test-read-only-secret-with-at-least-32-characters")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid machine request returned %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			ImageOutputs     int64 `json:"imageOutputs"`
			CoverageComplete bool  `json:"coverageComplete"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Code != 0 || payload.Data.ImageOutputs != 1 {
		t.Fatalf("unexpected aggregate: %+v, err=%v", payload, err)
	}
	for _, privateValue := range []string{"private-user", "resource", "prompt", "objectKey"} {
		if strings.Contains(response.Body.String(), privateValue) {
			t.Fatalf("aggregate leaked %q", privateValue)
		}
	}
}
