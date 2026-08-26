package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestRunComfyUIWorkflowTaskUsesWorkflowProtocolAndDownloadsOutput(t *testing.T) {
	var createBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer adapter-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/jobs":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"job-1","status":"submitted"}`))
		case "/v1/jobs/job-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"job-1","status":"succeeded","outputs":[{"index":0,"kind":"image","mimeType":"image/png","url":"/jobs/job-1/outputs/0"}]}`))
		case "/v1/jobs/job-1/outputs/0":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\ncanvas"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := providerConfig{BaseURL: server.URL + "/v1", APIKey: "adapter-token", Model: "qwen_image_t2i", InterfaceType: string(model.ChannelInterfaceComfyUIWorkflow), AllowLocalChannel: true, Size: "16:9", Count: "2", VideoGenerateAudio: "false"}
	ctx := withProviderOutboundPolicy(context.Background(), config)
	result, err := runComfyUIWorkflowTask(ctx, canvasGenerationInput{Mode: "image", Prompt: "cinematic rain", Config: config, Metadata: map[string]interface{}{"comfyProviderId": "gpu-3"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["mode"] != "image" {
		t.Fatalf("result = %#v", result)
	}
	if createBody["workflow_key"] != "qwen_image_t2i" || createBody["provider_id"] != "gpu-3" || createBody["batch_size"] != float64(2) {
		t.Fatalf("create body = %#v", createBody)
	}
	if createBody["width"] != float64(1792) || createBody["height"] != float64(1024) {
		t.Fatalf("dimensions = %v x %v", createBody["width"], createBody["height"])
	}
	if createBody["generate_audio"] != false {
		t.Fatalf("generate_audio = %#v, want false", createBody["generate_audio"])
	}
}

func TestComfyUIDimensionsAreStableAndAligned(t *testing.T) {
	tests := []struct {
		mode, size, quality string
		width, height       int
	}{
		{"image", "1024x768", "", 1024, 768},
		{"image", "9:16", "", 1024, 1792},
		{"video", "16:9", "1080", 1920, 1088},
	}
	for _, test := range tests {
		width, height := comfyUIDimensions(test.mode, test.size, test.quality)
		if width != test.width || height != test.height {
			t.Fatalf("%s %s: %dx%d, want %dx%d", test.mode, test.size, width, height, test.width, test.height)
		}
	}
}
