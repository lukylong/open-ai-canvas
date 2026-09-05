package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type comfyUIJobOutput struct {
	Index    int    `json:"index"`
	Kind     string `json:"kind"`
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type comfyUIJobState struct {
	ID               string             `json:"id"`
	PromptID         string             `json:"promptId"`
	ProviderID       string             `json:"providerId"`
	WorkflowKey      string             `json:"workflowKey"`
	WorkflowRevision string             `json:"workflowRevision"`
	Status           string             `json:"status"`
	Outputs          []comfyUIJobOutput `json:"outputs"`
	Error            string             `json:"error"`
}

func runComfyUIWorkflowTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	release, err := acquireComfyUIExecutionSlot(ctx, input)
	if err != nil {
		return nil, err
	}
	defer release()

	id := resumedProviderRequestID(ctx)
	if id == "" {
		width, height := comfyUIDimensions(input.Mode, input.Config.Size, input.Config.VQuality)
		images := make([]string, 0, len(input.ReferenceImages))
		for _, reference := range input.ReferenceImages {
			value, err := openAIImageInputURL(reference)
			if err != nil {
				return nil, fmt.Errorf("读取 ComfyUI 参考图失败：%w", err)
			}
			images = append(images, value)
		}
		body := map[string]interface{}{
			"provider_id":     defaultString(metadataString(input.Metadata, "comfyProviderId"), "default"),
			"workflow_key":    input.Config.Model,
			"prompt":          input.Prompt,
			"negative_prompt": metadataString(input.Metadata, "negativePrompt"),
			"input_images":    images,
			"width":           width,
			"height":          height,
			"batch_size":      comfyUIBatchSize(input.Config.Count),
			"generate_audio":  parseBool(input.Config.VideoGenerateAudio, true),
			"metadata":        input.Metadata,
		}
		if seconds := parseFloat(input.Config.VideoSeconds, 0); seconds > 0 {
			body["duration"] = seconds
		}
		var created comfyUIJobState
		if err := postJSON(ctx, input.Config, "/jobs", body, &created); err != nil {
			return nil, err
		}
		id = strings.TrimSpace(created.ID)
		if id == "" {
			return nil, errors.New("ComfyUI 工作流适配器没有返回任务 ID")
		}
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state comfyUIJobState
		if err := getJSON(ctx, input.Config, "/jobs/"+url.PathEscape(id), &state); err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(state.Status)) {
		case "succeeded":
			return comfyUIWorkflowResult(ctx, input, id, state.Outputs)
		case "failed":
			return nil, fmt.Errorf("ComfyUI 工作流 %s 执行失败：%s", input.Config.Model, defaultString(state.Error, "适配器返回失败"))
		case "cancelled", "canceled":
			return nil, context.Canceled
		case "submitted", "queued", "running", "processing", "":
		default:
			return nil, fmt.Errorf("ComfyUI 工作流任务 %s 返回未知状态：%s", id, state.Status)
		}
		if err := sleepContext(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, context.DeadlineExceeded
}

func acquireComfyUIExecutionSlot(ctx context.Context, input canvasGenerationInput) (func(), error) {
	metadata, ok := ctx.Value(providerAnalyticsKey{}).(providerAnalyticsContext)
	if !ok || metadata.Service == nil {
		return func() {}, nil
	}
	ttl := time.Until(providerPollingDeadline(ctx)) + time.Minute
	if ttl < time.Minute {
		ttl = time.Minute
	}
	fallbackScope := "comfyui:" + strings.ToLower(strings.TrimSpace(input.Config.BaseURL))
	if strings.TrimSpace(metadata.TaskID) != "" {
		_ = metadata.Service.repo.UpdateTaskProgress(metadata.TaskID, "等待可用生成节点", 0)
	}
	release, _, err := metadata.Service.AcquireChannelTaskSlot(ctx, metadata.ChannelID, fallbackScope, ttl)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(metadata.TaskID) != "" {
		_ = metadata.Service.repo.UpdateTaskProgress(metadata.TaskID, "正在连接上游", 0)
	}
	return release, nil
}

func comfyUIWorkflowResult(ctx context.Context, input canvasGenerationInput, id string, outputs []comfyUIJobOutput) (map[string]interface{}, error) {
	if len(outputs) == 0 {
		return nil, fmt.Errorf("ComfyUI 工作流任务 %s 已完成但没有输出", id)
	}
	if input.Mode == "image" {
		images := make([]map[string]interface{}, 0, len(outputs))
		for _, output := range outputs {
			if output.Kind != "image" || strings.TrimSpace(output.URL) == "" {
				continue
			}
			raw, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, comfyUIOutputURL(input.Config.BaseURL, output.URL))
			if err != nil {
				return nil, fmt.Errorf("下载 ComfyUI 图片输出失败：%w", err)
			}
			mimeType = normalizedMediaMimeType(firstNonEmpty(mimeType, output.MimeType), raw)
			images = append(images, map[string]interface{}{"dataUrl": dataURL(mimeType, raw), "mimeType": mimeType})
		}
		if len(images) == 0 {
			return nil, fmt.Errorf("ComfyUI 工作流任务 %s 没有图片输出", id)
		}
		return map[string]interface{}{"mode": "image", "images": images}, nil
	}
	for _, output := range outputs {
		if output.Kind != "video" || strings.TrimSpace(output.URL) == "" {
			continue
		}
		raw, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, comfyUIOutputURL(input.Config.BaseURL, output.URL))
		if err != nil {
			return nil, fmt.Errorf("下载 ComfyUI 视频输出失败：%w", err)
		}
		mimeType = normalizedMediaMimeType(firstNonEmpty(mimeType, output.MimeType), raw)
		return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(mimeType, raw), "mimeType": mimeType}}, nil
	}
	return nil, fmt.Errorf("ComfyUI 工作流任务 %s 没有视频输出", id)
}

func comfyUIOutputURL(baseURL string, outputURL string) string {
	value := strings.TrimSpace(outputURL)
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return apiURL(baseURL, "/"+strings.TrimLeft(value, "/"))
}

func comfyUIDimensions(mode string, size string, quality string) (int, int) {
	size = strings.ToLower(strings.TrimSpace(size))
	if parts := strings.Split(size, "x"); len(parts) == 2 {
		width, widthErr := strconv.Atoi(parts[0])
		height, heightErr := strconv.Atoi(parts[1])
		if widthErr == nil && heightErr == nil && width >= 64 && height >= 64 {
			return width, height
		}
	}
	base := 1024
	if mode == "video" {
		base = 720
		if strings.Contains(strings.ToLower(quality), "480") {
			base = 480
		} else if strings.Contains(strings.ToLower(quality), "1080") {
			base = 1080
		}
	}
	ratioWidth, ratioHeight := 1, 1
	if parts := strings.Split(size, ":"); len(parts) == 2 {
		if width, err := strconv.Atoi(parts[0]); err == nil && width > 0 {
			ratioWidth = width
		}
		if height, err := strconv.Atoi(parts[1]); err == nil && height > 0 {
			ratioHeight = height
		}
	}
	if ratioWidth >= ratioHeight {
		return roundComfyDimension(base * ratioWidth / ratioHeight), roundComfyDimension(base)
	}
	return roundComfyDimension(base), roundComfyDimension(base * ratioHeight / ratioWidth)
}

func roundComfyDimension(value int) int {
	if value < 64 {
		return 64
	}
	return max(64, ((value+32)/64)*64)
}

func comfyUIBatchSize(value string) int {
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || count < 1 {
		return 1
	}
	if count > 4 {
		return 4
	}
	return count
}
