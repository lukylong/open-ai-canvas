package service

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func gptFailureFixtures() []providerHTTPError {
	return []providerHTTPError{
		{StatusCode: 429, Body: `{"error":{"code":"rate_limit_exceeded","message":"secret"}}`, RetryAfter: 30 * time.Second},
		{StatusCode: 429, Body: `{"error":{"code":"insufficient_quota","message":"secret"}}`},
		{StatusCode: 503, Body: `{"error":{"message":"No available compatible accounts"}}`},
		{StatusCode: 400, Body: `{"error":{"message":"The 'gpt-5.4-mini' model is not supported when using Codex with a ChatGPT account."}}`},
		{StatusCode: 504, Body: `{"error":{"message":"secret"}}`},
	}
}

func TestGPTFailureProbe(t *testing.T) {
	for _, fixture := range gptFailureFixtures() {
		fmt.Printf("HTTP %d: %s\n", fixture.StatusCode, taskFailureMessage(fixture))
	}
}

func TestGPTFailureActionableHints(t *testing.T) {
	wants := []string{"30 秒", "额度不足", "暂无可用账号", "不支持", "重复计费"}
	for i, fixture := range gptFailureFixtures() {
		for _, message := range []string{fixture.Error(), providerUserFacingErrorMessage(fixture), taskFailureMessage(fmt.Errorf("图片生成：%w", fixture))} {
			if !strings.Contains(message, wants[i]) || strings.Contains(message, "secret") || strings.Contains(message, "gpt-5.4") {
				t.Errorf("fixture %d: %q, want safe hint %q", i, message, wants[i])
			}
		}
	}
}

func TestGPTFailureClassificationDoesNotScanUntrustedFields(t *testing.T) {
	for _, fixture := range []providerHTTPError{
		{StatusCode: 503, Body: `<html>No available compatible accounts secret</html>`},
		{StatusCode: 503, Body: `{"prompt":"No available compatible accounts","error":{"message":"secret"}}`},
		{StatusCode: 429, Body: `{"prompt":"insufficient quota","error":{"message":"secret"}}`},
		{StatusCode: 502, Body: `{"error":{"code":"no_available_accounts","message":"No available compatible accounts"}}`},
		{StatusCode: 429, Body: `{"error":{"message":"Draw text: insufficient quota secret"}}`},
	} {
		if hint := providerOperationalFailureHint(fixture); hint != "" {
			t.Errorf("unexpected classification: %q", hint)
		}
		if message := fixture.Error(); strings.Contains(message, "secret") || strings.Contains(message, "<html>") {
			t.Errorf("response leaked: %q", message)
		}
	}
}

func TestGPTFailureRetryAfterAndRouteSafety(t *testing.T) {
	if message := (providerHTTPError{StatusCode: 429, RetryAfter: 1500 * time.Millisecond}).Error(); !strings.Contains(message, "2 秒") {
		t.Fatalf("retry delay must round up: %q", message)
	}
	if message := (providerHTTPError{StatusCode: 429}).Error(); !strings.Contains(message, "请求限流") || strings.Contains(message, " 秒") {
		t.Fatalf("unknown retry delay must not be invented: %q", message)
	}
	for _, status := range []int{503, 504, 524} {
		if safeRouteRejection(providerHTTPError{StatusCode: status, Body: `{"error":{"code":"no_available_accounts"}}`}) {
			t.Fatalf("classification must not authorize automatic resubmission for %d", status)
		}
	}
}
