package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type CreateInvitationCodeRequest struct {
	Label     string     `json:"label"`
	MaxUses   int        `json:"maxUses"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type CreatedInvitationCode struct {
	model.InvitationCode
	Code string `json:"code"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func normalizeInvitationCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", " ", "").Replace(value)
}

func invitationCodeHash(value string) string {
	sum := sha256.Sum256([]byte(normalizeInvitationCode(value)))
	return hex.EncodeToString(sum[:])
}

func generateInvitationCode() (string, error) {
	var raw [9]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	value := strings.ToUpper(hex.EncodeToString(raw[:]))
	return "YC-" + value[:6] + "-" + value[6:12] + "-" + value[12:], nil
}

func (s *Service) AdminInvitationCodes(actor *model.User) ([]model.InvitationCode, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.InvitationCodes()
}

func (s *Service) CreateInvitationCode(actor *model.User, req CreateInvitationCodeRequest) (*CreatedInvitationCode, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	label := strings.TrimSpace(req.Label)
	if len([]rune(label)) > 60 {
		return nil, BadAuthRequest("邀请码备注不能超过 60 个字符")
	}
	if req.MaxUses < 0 || req.MaxUses > 10_000 {
		return nil, BadAuthRequest("邀请码使用次数必须在 0-10000 之间，0 表示不限次数")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, BadAuthRequest("邀请码过期时间必须晚于当前时间")
	}
	code, err := generateInvitationCode()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	invite := model.InvitationCode{
		ID: newID(), CodeHash: invitationCodeHash(code), CodePreview: "…" + code[len(code)-6:], Label: label,
		MaxUses: req.MaxUses, ExpiresAt: req.ExpiresAt, CreatedBy: actor.ID, CreatedAt: now, UpdatedAt: now,
	}
	audit, err := newAdminAuditEvent(actor, "invitation.create", "invitation_code", invite.ID, "创建邀请码", map[string]any{"maxUses": invite.MaxUses, "expiresAt": invite.ExpiresAt})
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateInvitationCodeWithAudit(&invite, audit); err != nil {
		return nil, err
	}
	return &CreatedInvitationCode{InvitationCode: invite, Code: code}, nil
}

func (s *Service) RevokeInvitationCode(actor *model.User, id string) (*model.InvitationCode, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	invite, err := s.repo.RevokeInvitationCode(strings.TrimSpace(id), time.Now())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("邀请码不存在或已经撤销")
	}
	if err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "invitation.revoke", "invitation_code", invite.ID, "撤销邀请码", nil); err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *Service) ChangePassword(user *model.User, req ChangePasswordRequest) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	if !verifyPassword(req.CurrentPassword, user.PasswordHash) {
		return Unauthorized("当前密码不正确")
	}
	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}
	if verifyPassword(req.NewPassword, user.PasswordHash) {
		return BadAuthRequest("新密码不能与当前密码相同")
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	return s.repo.UpdateUserPasswordAndRevokeSessions(user.ID, hash, time.Now())
}

func invitationError(err error) error {
	if errors.Is(err, repository.ErrInvitationInvalid) || errors.Is(err, gorm.ErrRecordNotFound) {
		return BadAuthRequest("邀请码无效、已过期或已用完")
	}
	return err
}
