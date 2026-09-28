// materials-smoke is a disposable local HTTPS fixture, not a production server.
package main

import (
	"encoding/json"
	"encoding/pem"
	"log"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/handler"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	if len(os.Args) != 2 || os.Getenv("CANVAS_ANALYTICS_READ_TOKEN") == "" {
		log.Fatal("usage: CANVAS_ANALYTICS_READ_TOKEN=<test-only-token> materials-smoke READY_DIRECTORY")
	}
	directory := os.Args[1]
	if err := os.MkdirAll(directory, 0o700); err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:" + filepath.Join(directory, "fixture.db")})
	if err != nil {
		log.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		log.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.GeneratedMediaOutput{TaskID: "fixture-task", ResourceID: "fixture-image", UserID: "fixture-user", Kind: "image", Model: "fixture-model", CreatedAt: now}).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Create(&model.Task{ID: "fixture-task", UserID: "fixture-user", Type: "canvas_image", Status: model.TaskStatusSucceeded, CreatedAt: now}).Error; err != nil {
		log.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterGeneratedMediaAnalyticsRoutes(router.Group("/api"), service.New(repository.New(db), directory))
	server := httptest.NewTLSServer(router)
	defer server.Close()
	if err := os.WriteFile(filepath.Join(directory, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		log.Fatal(err)
	}
	ready := struct {
		URL string `json:"url"`
	}{URL: server.URL + "/api/internal/v1/analytics/materials"}
	data, err := json.Marshal(ready)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "ready.json"), data, 0o600); err != nil {
		log.Fatal(err)
	}
	log.Printf("fixture ready at local HTTPS endpoint")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
