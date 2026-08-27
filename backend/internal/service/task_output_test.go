package service

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestTaskForOutputRedactsRoutingAndSecrets(t *testing.T) {
	task := model.Task{
		InputJSON:              `{"mode":"image","metadata":{"source":"create-page"},"config":{"apiKey":"secret"},"resourceId":"resource-1"}`,
		LogicalModelRevisionID: "revision-1",
		RouteID:                "route-1",
		ChannelModelID:         "channel-model-1",
	}

	output := taskForOutput(task)
	if output.LogicalModelRevisionID != "" || output.RouteID != "" || output.ChannelModelID != "" {
		t.Fatalf("internal routing fields leaked: %+v", output)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(output.InputJSON), &input); err != nil {
		t.Fatalf("public input is not valid JSON: %v", err)
	}
	if _, exists := input["config"]; exists {
		t.Fatal("provider config must not be exposed")
	}
	if input["resourceId"] != "resource-1" {
		t.Fatalf("resource identity was not preserved: %#v", input)
	}
}

func TestTaskSummaryForOutputExposesPublicLogicalModelOnly(t *testing.T) {
	task := model.Task{
		ID:                     "task-1",
		LogicalModelID:         "logical-model-1",
		LogicalModelRevisionID: "revision-1",
		RouteID:                "route-1",
		ChannelModelID:         "channel-model-1",
	}

	output := taskSummaryForOutput(task)
	if output.LogicalModelID != task.LogicalModelID {
		t.Fatalf("logical model id missing from task summary: %+v", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal task summary: %v", err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatalf("decode task summary: %v", err)
	}
	for _, protected := range []string{"logicalModelRevisionId", "routeId", "channelModelId"} {
		if _, exists := public[protected]; exists {
			t.Fatalf("protected routing field %q leaked: %s", protected, encoded)
		}
	}
}

func TestTaskMediaPreviewUsesSafeMediaURLs(t *testing.T) {
	previewURL, previewKind := taskMediaPreview(`{"images":["data:image/png;base64,AAAA","/api/resources/resource-1/file"],"video":"https://cdn.example.com/output.mp4"}`, "video")
	if previewURL != "/api/resources/resource-1/file" || previewKind != "image" {
		t.Fatalf("unexpected preview: url=%q kind=%q", previewURL, previewKind)
	}
	if previewURL, _ := taskMediaPreview(`{"url":"file:///tmp/output.mp4"}`, "video"); previewURL != "" {
		t.Fatalf("unsafe local URL was exposed: %q", previewURL)
	}
}

func TestTaskClientContextRequiresCreatePageMetadata(t *testing.T) {
	valid := taskClientContext(`{"metadata":{"source":"create-page","conversationId":"conversation-1","messageId":"message-1","batchIndex":2,"batchCount":4,"seriesId":"creation-series:operation-1"}}`)
	if valid == nil || valid.ConversationID != "conversation-1" || valid.BatchIndex != 2 || valid.SeriesID != "creation-series:operation-1" {
		t.Fatalf("valid client context was not decoded: %+v", valid)
	}
	if context := taskClientContext(`{"metadata":{"source":"other","conversationId":"conversation-1","messageId":"message-1"}}`); context != nil {
		t.Fatalf("unexpected context for non-create-page task: %+v", context)
	}
}
