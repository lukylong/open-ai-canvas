package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBeginIAMOIDCLoginCreatesOneTimeStateWithPKCEAndNonce(t *testing.T) {
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer provider.Close()
	issuer = provider.URL
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("CANVAS_IAM_OIDC_ENABLED", "true")
	t.Setenv("CANVAS_IAM_OIDC_ISSUER", provider.URL)
	t.Setenv("CANVAS_IAM_OIDC_CLIENT_ID", "canvas")
	t.Setenv("CANVAS_IAM_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CANVAS_IAM_OIDC_REDIRECT_URL", "http://127.0.0.1:3000/api/auth/iam/callback")
	t.Setenv("CANVAS_IAM_API_URL", provider.URL)
	t.Setenv("CANVAS_IAM_APPLICATION_TOKEN", "application-token")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.OAuthState{}); err != nil {
		t.Fatal(err)
	}
	target, err := (&Service{repo: repository.New(db)}).BeginIAMOIDCLogin(context.Background(), "/create?source=iam")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Path != "/authorize" || query.Get("client_id") != "canvas" || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("nonce") == "" || query.Get("state") == "" {
		t.Fatalf("authorization URL = %s", target)
	}
	var state model.OAuthState
	if err := db.First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.Provider != iamOIDCProvider || state.NonceHash == "" || state.CodeVerifier == "" || state.NextPath != "/create?source=iam" {
		t.Fatalf("stored state = %#v", state)
	}
	digest := sha256.Sum256([]byte(state.CodeVerifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatalf("code_challenge does not match stored verifier")
	}
}

func TestLoadIAMOIDCConfigRequiresCompleteEnabledConfiguration(t *testing.T) {
	t.Setenv("CANVAS_IAM_OIDC_ENABLED", "true")
	for _, name := range []string{"CANVAS_IAM_OIDC_ISSUER", "CANVAS_IAM_OIDC_CLIENT_ID", "CANVAS_IAM_OIDC_CLIENT_SECRET", "CANVAS_IAM_OIDC_REDIRECT_URL", "CANVAS_IAM_API_URL", "CANVAS_IAM_APPLICATION_TOKEN"} {
		t.Setenv(name, "")
	}
	if _, err := loadIAMOIDCConfig(); err == nil || !strings.Contains(err.Error(), "配置不完整") {
		t.Fatalf("loadIAMOIDCConfig() error = %v", err)
	}
}

func TestLoadIAMOIDCConfigDisabledNeedsNoSecrets(t *testing.T) {
	t.Setenv("CANVAS_IAM_OIDC_ENABLED", "false")
	config, err := loadIAMOIDCConfig()
	if err != nil || config.Enabled {
		t.Fatalf("loadIAMOIDCConfig() = %#v, %v", config, err)
	}
}

func TestCreateIAMOIDCUserCreatesOrdinaryIsolatedAccount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	existing := model.User{ID: "existing", Username: "alice", Email: "alice@example.com", DisplayName: "Existing", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	provisioning := &iamProvisioningContext{}
	provisioning.Employee.ID = "employee-1"
	provisioning.Employee.OIDCSubject = "subject-1"
	provisioning.Employee.Username = "alice"
	provisioning.Employee.DisplayName = "Alice IAM"
	provisioning.Employee.Email = "alice@example.com"

	user, identity, err := (&Service{repo: repository.New(db)}).createIAMOIDCUser(provisioning, iamIDTokenClaims{Subject: "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != model.UserRoleUser || user.Username == existing.Username || user.Email != "" {
		t.Fatalf("created user = %#v", user)
	}
	if identity.Provider != iamOIDCProvider || identity.Subject != "subject-1" || identity.UserID != user.ID {
		t.Fatalf("created identity = %#v", identity)
	}
}

func TestAttachIAMOIDCIdentityUsesVerifiedExistingBinding(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "legacy-user", Username: "legacy", DisplayName: "Legacy", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	provisioning := &iamProvisioningContext{}
	provisioning.Employee.Username = "employee"
	attachedUser, identity, err := (&Service{repo: repository.New(db)}).attachIAMOIDCIdentity(user.ID, provisioning, iamIDTokenClaims{Subject: "iam-subject"})
	if err != nil {
		t.Fatal(err)
	}
	if attachedUser.ID != user.ID || identity.UserID != user.ID || identity.Subject != "iam-subject" || identity.Provider != iamOIDCProvider {
		t.Fatalf("attached user=%#v identity=%#v", attachedUser, identity)
	}
}

func TestAttachIAMOIDCIdentityRejectsMissingBindingTarget(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	_, _, err = (&Service{repo: repository.New(db)}).attachIAMOIDCIdentity("missing-user", &iamProvisioningContext{}, iamIDTokenClaims{Subject: "iam-subject"})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("attachIAMOIDCIdentity() error = %v", err)
	}
}

func TestPublicAuthUserPrefersIAMIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "user-1", Username: "canvas", DisplayName: "Canvas", Role: model.UserRoleUser, Status: model.UserStatusActive}
	identities := []model.UserIdentity{
		{ID: "linux", UserID: user.ID, Provider: "linuxdo", Subject: "linux-sub", ProviderUsername: "linux-user"},
		{ID: "iam", UserID: user.ID, Provider: iamOIDCProvider, Subject: "iam-sub", ProviderUsername: "iam-user"},
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	result, err := (&Service{repo: repository.New(db)}).PublicAuthUser(&user)
	if err != nil {
		t.Fatal(err)
	}
	if result.IdentityProvider != iamOIDCProvider || result.IdentityID != "iam-sub" || result.IdentityUsername != "iam-user" {
		t.Fatalf("PublicAuthUser() = %#v", result)
	}
}
