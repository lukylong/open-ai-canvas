package zqmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

type COSConfig struct {
	Bucket           string
	Region           string
	Domain           string
	InternalEndpoint string
	AccessKeyID      string
	AccessKeySecret  string
	PublicRead       bool
}

func normalizeSourceDSN(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"postgresql+asyncpg://", "postgres+asyncpg://", "postgresql+psycopg://"} {
		if strings.HasPrefix(value, prefix) {
			return "postgresql://" + strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

func normalizeInvite(value string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(value)))
}

func invitationHash(value string) string {
	sum := sha256.Sum256([]byte(normalizeInvite(value)))
	return hex.EncodeToString(sum[:])
}

func deterministicID(prefix string, sourceID string) string {
	candidate := prefix + strings.TrimSpace(sourceID)
	if len(candidate) <= 36 {
		return candidate
	}
	sum := sha256.Sum256([]byte(candidate))
	return prefix + hex.EncodeToString(sum[:])[:36-len(prefix)]
}

func (config COSConfig) normalize() COSConfig {
	config.Bucket = strings.Trim(strings.TrimSpace(config.Bucket), "/")
	config.Region = strings.TrimSpace(config.Region)
	if config.Region == "" {
		config.Region = "ap-guangzhou"
	}
	config.Domain = strings.TrimRight(strings.TrimSpace(config.Domain), "/")
	config.InternalEndpoint = strings.TrimRight(strings.TrimSpace(config.InternalEndpoint), "/")
	config.AccessKeyID = strings.TrimSpace(config.AccessKeyID)
	config.AccessKeySecret = strings.TrimSpace(config.AccessKeySecret)
	return config
}

func (config COSConfig) publicDomain() string {
	config = config.normalize()
	if config.Domain != "" {
		return config.Domain
	}
	if config.Bucket == "" {
		return ""
	}
	return "https://" + config.Bucket + ".cos." + config.Region + ".myqcloud.com"
}

func (config COSConfig) bucketQualifiedEndpoint() bool {
	config = config.normalize()
	endpoint := config.InternalEndpoint
	if endpoint == "" {
		endpoint = config.publicDomain()
	}
	if endpoint == "" || config.Bucket == "" {
		return false
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	want := strings.ToLower(config.Bucket + ".cos." + config.Region + ".myqcloud.com")
	return strings.EqualFold(parsed.Hostname(), want)
}

func metadataString(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}

// physicalCOSObjectKey reproduces the source's bucket-qualified S3 endpoint behavior once.
func physicalCOSObjectKey(asset SourceAsset, config COSConfig) string {
	config = config.normalize()
	key := metadataString(asset.AssetMetadata, "object_key")
	if key == "" {
		key = strings.TrimSpace(asset.StorageKey)
	}
	raw := strings.TrimSpace(metadataString(asset.AssetMetadata, "locator"))
	if raw == "" {
		raw = strings.TrimSpace(asset.URL)
	}
	if strings.HasPrefix(raw, "cos://") {
		locator := strings.TrimPrefix(raw, "cos://")
		parts := strings.SplitN(locator, "/", 2)
		if len(parts) == 2 && (config.Bucket == "" || parts[0] == config.Bucket) {
			key = parts[1]
		}
	} else if (strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")) && config.publicDomain() != "" {
		if parsed, err := url.Parse(raw); err == nil {
			domain, _ := url.Parse(config.publicDomain())
			if domain != nil && strings.EqualFold(parsed.Hostname(), domain.Hostname()) {
				key = strings.TrimPrefix(parsed.EscapedPath(), "/")
				if decoded, decodeErr := url.PathUnescape(key); decodeErr == nil {
					key = decoded
				}
			}
		}
	}
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if config.bucketQualifiedEndpoint() && key != "" && config.Bucket != "" && !strings.HasPrefix(key, config.Bucket+"/") {
		key = config.Bucket + "/" + key
	}
	return key
}

func physicalCOSPublicURL(asset SourceAsset, objectKey string, config COSConfig) string {
	raw := strings.TrimSpace(asset.URL)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if objectKey == "" || strings.Contains(raw, url.PathEscape(objectKey)) {
			return raw
		}
	}
	domain := config.publicDomain()
	if domain == "" || objectKey == "" {
		return raw
	}
	parts := strings.Split(objectKey, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return domain + "/" + strings.Join(parts, "/")
}
