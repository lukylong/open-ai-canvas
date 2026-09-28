// Source-owned read-only API, independently deployed without restarting generation.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const path = "/api/internal/v1/analytics/materials"

type exporter func(context.Context, string, string) (json.RawMessage, error)
type cached struct {
	body json.RawMessage
	at   time.Time
}

func handler(token string, export exporter) http.Handler {
	expected := sha256.Sum256([]byte("Bearer " + token))
	gate := make(chan struct{}, 2)
	var mutex sync.Mutex
	cache := map[string]cached{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fail := func(status int) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"code":%d,"msg":"analytics request unavailable"}`, status)
		}
		if r.Method != http.MethodGet {
			fail(405)
			return
		}
		if r.URL.Path == "/healthz" {
			w.Write([]byte(`{"ok":true,"service":"canvas-analytics","mode":"read-only"}`))
			return
		}
		if r.URL.Path != path {
			fail(404)
			return
		}
		given := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(token) < 32 || subtle.ConstantTimeCompare(expected[:], given[:]) != 1 {
			fail(401)
			return
		}
		query := r.URL.Query()
		for k, v := range query {
			if (k != "from" && k != "to") || len(v) != 1 {
				fail(400)
				return
			}
		}
		zone, _ := time.LoadLocation("Asia/Shanghai")
		today := time.Now().In(zone).Format("2006-01-02")
		from, to := query.Get("from"), query.Get("to")
		if from == "" && to == "" {
			now, _ := time.ParseInLocation("2006-01-02", today, zone)
			from = now.AddDate(0, 0, -6).Format("2006-01-02")
			to = today
		}
		a, e1 := time.Parse("2006-01-02", from)
		b, e2 := time.Parse("2006-01-02", to)
		if e1 != nil || e2 != nil || a.Format("2006-01-02") != from || b.Format("2006-01-02") != to || b.Before(a) || b.Sub(a) > 365*24*time.Hour || to > today {
			fail(400)
			return
		}
		key := from + ":" + to
		mutex.Lock()
		c, ok := cache[key]
		mutex.Unlock()
		if ok && time.Since(c.at) < 30*time.Second {
			w.Write(c.body)
			return
		}
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		default:
			fail(429)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		body, err := export(ctx, from, to)
		if err != nil || len(body) > 1<<20 {
			log.Printf("readonly statistics failed: %T", err)
			fail(503)
			return
		}
		var envelope struct {
			Code int            `json:"code"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Code != 0 || envelope.Data == nil {
			fail(503)
			return
		}
		envelope.Data["dataOrigin"] = "source_api"
		envelope.Data["readOnly"] = true
		envelope.Data["metricVersion"] = "generation-task-cohort-v1"
		envelope.Data["scope"] = "global_aggregate"
		delete(envelope.Data, "previewTransaction")
		warnings := []any{}
		if list, ok := envelope.Data["warnings"].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok && !strings.Contains(s, "正式统计API尚未发布") {
					warnings = append(warnings, s)
				}
			}
		}
		envelope.Data["warnings"] = warnings
		body, err = json.Marshal(envelope)
		if err != nil {
			fail(503)
			return
		}
		mutex.Lock()
		if len(cache) >= 16 {
			cache = map[string]cached{}
		}
		cache[key] = cached{body, time.Now()}
		mutex.Unlock()
		w.Write(body)
	})
}
func main() {
	token := os.Getenv("CANVAS_ANALYTICS_READ_TOKEN")
	if len(token) < 32 {
		log.Fatal("missing read-only service credential")
	}
	database := os.Getenv("ANALYTICS_SQLITE_PATH")
	if database == "" {
		database = "/source/open_ai_canvas.db"
	}
	script := os.Getenv("ANALYTICS_EXPORT_SCRIPT")
	if script == "" {
		script = "/app/export_task_analytics.py"
	}
	export := func(ctx context.Context, from, to string) (json.RawMessage, error) {
		return exec.CommandContext(ctx, "python3", script, database, from, to).Output()
	}
	server := &http.Server{Addr: ":8000", Handler: handler(token, export), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 12 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	log.Print("source-owned read-only analytics listening on :8000")
	log.Fatal(server.ListenAndServe())
}
