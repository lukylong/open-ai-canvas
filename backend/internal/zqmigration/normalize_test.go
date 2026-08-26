package zqmigration

import "testing"

func TestPhysicalCOSObjectKeyPreservesBucketQualifiedPhysicalPath(t *testing.T) {
	asset := SourceAsset{StorageKey: "media/account/job/image.png", URL: "cos://bucket-123/media/account/job/image.png"}
	config := COSConfig{Bucket: "bucket-123", Region: "ap-guangzhou", Domain: "https://bucket-123.cos.ap-guangzhou.myqcloud.com"}
	if got := physicalCOSObjectKey(asset, config); got != "bucket-123/media/account/job/image.png" {
		t.Fatalf("physicalCOSObjectKey() = %q", got)
	}
	if got := physicalCOSObjectKey(SourceAsset{StorageKey: "bucket-123/media/account/job/image.png"}, config); got != "bucket-123/media/account/job/image.png" {
		t.Fatalf("already normalized key = %q", got)
	}
}

func TestPhysicalCOSObjectKeyKeepsLogicalPathForPathStyleEndpoint(t *testing.T) {
	asset := SourceAsset{StorageKey: "media/account/job/image.png", URL: "cos://bucket-123/media/account/job/image.png"}
	config := COSConfig{Bucket: "bucket-123", Region: "ap-guangzhou", Domain: "https://bucket-123.cos.ap-guangzhou.myqcloud.com", InternalEndpoint: "https://cos.ap-guangzhou.myqcloud.com"}
	if got := physicalCOSObjectKey(asset, config); got != "media/account/job/image.png" {
		t.Fatalf("physicalCOSObjectKey() = %q", got)
	}
}

func TestInvitationHashNormalizesFormatting(t *testing.T) {
	if invitationHash("yc-abcd-1234") != invitationHash("YC ABCD1234") {
		t.Fatal("formatted invitation codes should have the same digest")
	}
}

func TestTargetVoiceStatusMapsReadyProfileToActive(t *testing.T) {
	if got := targetVoiceStatus(SourceVoiceProfile{Status: "ready"}); got != "active" {
		t.Fatalf("status = %q", got)
	}
	if got := targetVoiceStatus(SourceVoiceProfile{Status: "processing"}); got != "processing" {
		t.Fatalf("status = %q", got)
	}
}
