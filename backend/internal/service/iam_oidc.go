package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

const iamOIDCProvider = "iam-hub"

type iamOIDCConfig struct {
	Enabled          bool
	IssuerURL        string
	ClientID         string
	ClientSecret     string
	RedirectURL      string
	IAMAPIURL        string
	ApplicationID    string
	ApplicationToken string
	Scopes           []string
}

type IAMOIDCCallbackResult struct {
	Session *AuthSessionResult
	Next    string
}

type iamProvisioningContext struct {
	Employee struct {
		ID          string `json:"id"`
		OIDCSubject string `json:"oidcSubject"`
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Status      string `json:"status"`
	} `json:"employee"`
	ApplicationID            string `json:"applicationId"`
	LocalIdentityKey         string `json:"localIdentityKey"`
	ExternalAccountID        string `json:"externalAccountId"`
	ShouldCreateLocalAccount bool   `json:"shouldCreateLocalAccount"`
}

type iamIDTokenClaims struct {
	Subject           string `json:"sub"`
	Nonce             string `json:"nonce"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Email             string `json:"email"`
}

func (s *Service) IAMOIDCEnabled() bool {
	config, err := loadIAMOIDCConfig()
	return err == nil && config.Enabled
}

func (s *Service) BeginIAMOIDCLogin(ctx context.Context, nextPath string) (string, error) {
	config, err := loadIAMOIDCConfig()
	if err != nil {
		return "", err
	}
	if !config.Enabled {
		return "", Forbidden("IAM Hub 快捷登录尚未启用")
	}
	provider, _, err := discoverIAMOIDCProvider(ctx, config)
	if err != nil {
		return "", err
	}

	stateValue := randomToken()
	verifier := randomToken()
	nonce := randomToken()
	if err := s.repo.CreateOAuthState(&model.OAuthState{
		ID:           newID(),
		Provider:     iamOIDCProvider,
		StateHash:    hashToken(stateValue),
		CodeVerifier: verifier,
		NonceHash:    hashToken(nonce),
		NextPath:     safeOAuthNext(nextPath),
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}); err != nil {
		return "", err
	}

	oauthConfig := iamOAuth2Config(config, provider)
	return oauthConfig.AuthCodeURL(
		stateValue,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
		oauth2.AccessTypeOnline,
	), nil
}

func (s *Service) CompleteIAMOIDCLogin(ctx context.Context, stateValue string, code string) (*IAMOIDCCallbackResult, error) {
	if strings.TrimSpace(stateValue) == "" || strings.TrimSpace(code) == "" {
		return nil, BadAuthRequest("IAM Hub 登录回调缺少必要参数")
	}
	state, err := s.repo.ConsumeOAuthState(iamOIDCProvider, hashToken(stateValue))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("IAM Hub 登录状态无效或已过期")
	}
	if err != nil {
		return nil, err
	}
	config, err := loadIAMOIDCConfig()
	if err != nil {
		return nil, err
	}
	provider, providerContext, err := discoverIAMOIDCProvider(ctx, config)
	if err != nil {
		return nil, err
	}
	oauthConfig := iamOAuth2Config(config, provider)
	token, err := oauthConfig.Exchange(providerContext, code, oauth2.VerifierOption(state.CodeVerifier))
	if err != nil {
		return nil, fmt.Errorf("IAM Hub Token 交换失败：%w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return nil, errors.New("IAM Hub Token 响应缺少 ID Token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(providerContext, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("IAM Hub ID Token 校验失败：%w", err)
	}
	var claims iamIDTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, errors.New("IAM Hub ID Token Claims 无效")
	}
	if claims.Subject == "" || state.NonceHash == "" || hashToken(claims.Nonce) != state.NonceHash {
		return nil, BadAuthRequest("IAM Hub 登录 nonce 校验失败")
	}

	provisioning, err := fetchIAMProvisioningContext(providerContext, config, claims.Subject)
	if err != nil {
		return nil, err
	}
	if provisioning.ApplicationID != config.ApplicationID || provisioning.LocalIdentityKey != claims.Subject || provisioning.ShouldCreateLocalAccount != (provisioning.ExternalAccountID == "") {
		return nil, errors.New("IAM Hub 权限响应与当前应用不一致")
	}
	if provisioning.Employee.Status != "active" || provisioning.Employee.OIDCSubject != claims.Subject {
		return nil, Forbidden("IAM Hub 员工账号不可用")
	}

	identity, identityErr := s.repo.UserIdentity(iamOIDCProvider, claims.Subject)
	var user *model.User
	if identityErr == nil {
		if provisioning.ExternalAccountID != "" && provisioning.ExternalAccountID != identity.UserID {
			return nil, Forbidden("IAM Hub 账号绑定与影策身份不一致")
		}
		user, err = s.repo.User(identity.UserID)
		if err != nil {
			return nil, err
		}
		identity.ProviderUsername = firstNonEmpty(provisioning.Employee.Username, claims.PreferredUsername)
		identity.UpdatedAt = time.Now()
		if err := s.repo.Save(identity); err != nil {
			return nil, err
		}
	} else if errors.Is(identityErr, gorm.ErrRecordNotFound) {
		if provisioning.ExternalAccountID != "" {
			user, identity, err = s.attachIAMOIDCIdentity(provisioning.ExternalAccountID, provisioning, claims)
			if err != nil {
				return nil, err
			}
			if err := s.repo.CreateUserIdentity(identity); err != nil {
				return nil, err
			}
		} else {
			user, identity, err = s.createIAMOIDCUser(provisioning, claims)
			if err != nil {
				return nil, err
			}
			if err := s.repo.CreateOAuthUser(user, identity); err != nil {
				return nil, err
			}
		}
	} else {
		return nil, identityErr
	}
	if user.Status != model.UserStatusActive {
		return nil, Forbidden("影策本地账号已被禁用")
	}
	if provisioning.ExternalAccountID == "" {
		if err := bindIAMExternalAccount(providerContext, config, claims.Subject, user.ID); err != nil {
			return nil, err
		}
	}
	if err := s.ensureSignupBonus(user.ID); err != nil {
		return nil, err
	}
	now := time.Now()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.repo.Save(user); err != nil {
		return nil, err
	}
	s.recordActivity(user.ID, "login", 1)
	session, err := s.createAuthSession(user)
	if err != nil {
		return nil, err
	}
	return &IAMOIDCCallbackResult{Session: session, Next: safeOAuthNext(state.NextPath)}, nil
}

func (s *Service) attachIAMOIDCIdentity(userID string, provisioning *iamProvisioningContext, claims iamIDTokenClaims) (*model.User, *model.UserIdentity, error) {
	user, err := s.repo.User(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, Forbidden("IAM Hub 绑定的影策账号不存在")
	}
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.repo.UserIdentityForUser(user.ID, iamOIDCProvider)
	if err == nil && existing.Subject != claims.Subject {
		return nil, nil, Forbidden("影策账号已绑定其他 IAM Hub 员工")
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	identity := &model.UserIdentity{ID: newID(), UserID: user.ID, Provider: iamOIDCProvider, Subject: claims.Subject, ProviderUsername: firstNonEmpty(provisioning.Employee.Username, claims.PreferredUsername)}
	return user, identity, nil
}

func (s *Service) createIAMOIDCUser(provisioning *iamProvisioningContext, claims iamIDTokenClaims) (*model.User, *model.UserIdentity, error) {
	base := oauthUsernameSanitizer.ReplaceAllString(strings.TrimSpace(firstNonEmpty(provisioning.Employee.Username, claims.PreferredUsername)), "_")
	base = strings.Trim(base, "_-")
	if len(base) < 3 {
		base = "iam_" + shortSubject(claims.Subject)
	}
	if len(base) > 24 {
		base = base[:24]
	}
	username := base
	if existing, err := s.repo.UserByUsername(username); err == nil && existing != nil {
		username = truncateRunes(base, 23) + "_" + shortSubject(claims.Subject)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	email := normalizeEmail(firstNonEmpty(provisioning.Employee.Email, claims.Email))
	if email != "" {
		if validateEmail(email) != nil {
			email = ""
		} else if existing, err := s.repo.UserByEmail(email); err == nil && existing != nil {
			email = ""
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
	}
	displayName := firstNonEmpty(provisioning.Employee.DisplayName, claims.Name, username)
	user := &model.User{ID: newID(), Username: username, Email: email, DisplayName: normalizeDisplayName(displayName, username), Role: model.UserRoleUser, Status: model.UserStatusActive}
	identity := &model.UserIdentity{ID: newID(), UserID: user.ID, Provider: iamOIDCProvider, Subject: claims.Subject, ProviderUsername: provisioning.Employee.Username}
	return user, identity, nil
}

func loadIAMOIDCConfig() (iamOIDCConfig, error) {
	config := iamOIDCConfig{
		Enabled:          envEnabled("CANVAS_IAM_OIDC_ENABLED"),
		IssuerURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("CANVAS_IAM_OIDC_ISSUER")), "/"),
		ClientID:         strings.TrimSpace(os.Getenv("CANVAS_IAM_OIDC_CLIENT_ID")),
		ClientSecret:     strings.TrimSpace(os.Getenv("CANVAS_IAM_OIDC_CLIENT_SECRET")),
		RedirectURL:      strings.TrimSpace(os.Getenv("CANVAS_IAM_OIDC_REDIRECT_URL")),
		IAMAPIURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("CANVAS_IAM_API_URL")), "/"),
		ApplicationID:    firstNonEmpty(strings.TrimSpace(os.Getenv("CANVAS_IAM_APPLICATION_ID")), "open-ai-canvas"),
		ApplicationToken: strings.TrimSpace(os.Getenv("CANVAS_IAM_APPLICATION_TOKEN")),
		Scopes:           []string{oidc.ScopeOpenID, "profile", "email"},
	}
	if !config.Enabled {
		return config, nil
	}
	if config.IssuerURL == "" || config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" || config.IAMAPIURL == "" || config.ApplicationToken == "" {
		return config, BadAuthRequest("IAM Hub OIDC 配置不完整")
	}
	if _, err := ValidateOutboundURL(config.IssuerURL); err != nil {
		return config, err
	}
	if _, err := ValidateOutboundURL(config.IAMAPIURL); err != nil {
		return config, err
	}
	redirect, err := url.Parse(config.RedirectURL)
	if err != nil || redirect.Host == "" || (redirect.Scheme != "https" && !(redirect.Scheme == "http" && isLoopbackOAuthHost(redirect.Hostname()))) {
		return config, BadAuthRequest("IAM Hub 回调地址必须使用 HTTPS，本地回环地址可使用 HTTP")
	}
	return config, nil
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func discoverIAMOIDCProvider(ctx context.Context, config iamOIDCConfig) (*oidc.Provider, context.Context, error) {
	if _, err := ValidateOutboundURL(config.IssuerURL); err != nil {
		return nil, ctx, err
	}
	providerContext := oidc.ClientContext(ctx, OutboundHTTPClient(20*time.Second))
	provider, err := oidc.NewProvider(providerContext, config.IssuerURL)
	if err != nil {
		return nil, providerContext, fmt.Errorf("IAM Hub OIDC discovery 失败：%w", err)
	}
	return provider, providerContext, nil
}

func iamOAuth2Config(config iamOIDCConfig, provider *oidc.Provider) oauth2.Config {
	return oauth2.Config{ClientID: config.ClientID, ClientSecret: config.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: config.RedirectURL, Scopes: config.Scopes}
}

func fetchIAMProvisioningContext(ctx context.Context, config iamOIDCConfig, subject string) (*iamProvisioningContext, error) {
	target := fmt.Sprintf("%s/internal/v1/applications/%s/employees/%s/provisioning-context", config.IAMAPIURL, url.PathEscape(config.ApplicationID), url.PathEscape(subject))
	if _, err := ValidateOutboundURL(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.ApplicationToken)
	req.Header.Set("Accept", "application/json")
	ApplyDefaultOutboundHeaders(req)
	resp, err := OutboundHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("IAM Hub 权限校验失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, Forbidden("当前员工未获得影策访问权限")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("IAM Hub 权限校验失败：HTTP %d", resp.StatusCode)
	}
	var result iamProvisioningContext
	if err := json.Unmarshal(body, &result); err != nil || result.Employee.ID == "" {
		return nil, errors.New("IAM Hub 权限响应无效")
	}
	return &result, nil
}

func bindIAMExternalAccount(ctx context.Context, config iamOIDCConfig, subject string, localUserID string) error {
	target := fmt.Sprintf("%s/internal/v1/applications/%s/bindings", config.IAMAPIURL, url.PathEscape(config.ApplicationID))
	payload, _ := json.Marshal(map[string]string{"employeeSubject": subject, "externalAccountId": localUserID, "verificationMethod": "oidc"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(config.ApplicationID + ":" + subject + ":" + localUserID))
	req.Header.Set("Authorization", "Bearer "+config.ApplicationToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", newID())
	req.Header.Set("Idempotency-Key", "oidc-"+hex.EncodeToString(digest[:16]))
	ApplyDefaultOutboundHeaders(req)
	resp, err := OutboundHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("IAM Hub 账号绑定失败：%w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("IAM Hub 账号绑定失败：HTTP %d", resp.StatusCode)
	}
	return nil
}
