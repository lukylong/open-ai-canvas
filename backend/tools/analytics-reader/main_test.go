package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestReadOnlyHTTPContract(t *testing.T) {
	token := "fixture-readonly-source-credential-32-characters"
	calls := 0
	h := handler(token, func(_ context.Context, from, to string) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"code":0,"data":{"generationTasks":3,"imageOutputs":null,"previewTransaction":{},"warnings":["任务不是产物","正式统计API尚未发布"]}}`), nil
	})
	cases := []struct {
		url, token, method string
		status             int
	}{{path, "", "GET", 401}, {path, "wrong", "GET", 401}, {path + "?from=2026-09-01&to=2026-09-28", token, "GET", 200}, {path + "?from=2026-09-01&to=2026-09-28", token, "GET", 200}, {path + "?from=2026-09-31&to=2026-09-31", token, "GET", 400}, {path + "?userId=1", token, "GET", 400}, {path + "?from=1&from=2", token, "GET", 400}, {"/api/users", token, "GET", 404}, {path, token, "POST", 405}}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.url, nil)
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatalf("%s got %d want %d", c.url, w.Code, c.status)
		}
		if w.Code == 200 {
			var e struct{ Data map[string]any }
			if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if e.Data["dataOrigin"] != "source_api" || e.Data["imageOutputs"] != nil {
				t.Fatal(e.Data)
			}
		}
	}
	if calls != 1 {
		t.Fatalf("cache/export calls=%d", calls)
	}
}
