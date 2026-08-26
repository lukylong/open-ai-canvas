package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestReadOnlyPostgresDSNAppliesConnectionWideOption(t *testing.T) {
	value := readOnlyPostgresDSN("postgresql+asyncpg://user:pass@db.example/zq?sslmode=require")
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "postgresql" || parsed.Query().Get("options") != "-c default_transaction_read_only=on" || parsed.Query().Get("sslmode") != "require" {
		t.Fatalf("read-only DSN = %q", value)
	}
	keyword := readOnlyPostgresDSN("host=db.example dbname=zq")
	if !strings.Contains(keyword, "default_transaction_read_only=on") {
		t.Fatalf("keyword read-only DSN = %q", keyword)
	}
}

func TestSourceCOSConfigMapsCredentialsWithoutLoggingThem(t *testing.T) {
	config := sourceCOSConfig(map[string]string{
		"QCLOUD_COS_BUCKET":      "bucket-1",
		"QCLOUD_COS_REGION":      "ap-guangzhou",
		"QCLOUD_COS_DOMAIN":      "https://bucket-1.cos.ap-guangzhou.myqcloud.com",
		"QCLOUD_COS_SECRET_ID":   "secret-id",
		"QCLOUD_COS_ACCESS_KEY":  "legacy-id",
		"QCLOUD_COS_SECRET_KEY":  "secret-key",
		"QCLOUD_COS_PUBLIC_READ": "true",
	})
	if config.Bucket != "bucket-1" || config.AccessKeyID != "secret-id" || config.AccessKeySecret != "secret-key" || !config.PublicRead {
		t.Fatalf("COS config mapping failed: bucket=%q hasAccessKeyID=%t hasSecret=%t publicRead=%t", config.Bucket, config.AccessKeyID != "", config.AccessKeySecret != "", config.PublicRead)
	}
	legacy := sourceCOSConfig(map[string]string{"QCLOUD_COS_ACCESS_KEY": "legacy-id"})
	if legacy.AccessKeyID != "legacy-id" {
		t.Fatalf("legacy COS access key = %q", legacy.AccessKeyID)
	}
}
