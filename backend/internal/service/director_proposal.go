package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type DirectorPromptCandidate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Prompt      string `json:"prompt"`
	Recommended bool   `json:"recommended"`
}

type DirectorPromptProposalSummary struct {
	ID             string                    `json:"id"`
	ProjectID      string                    `json:"projectId,omitempty"`
	SourceText     string                    `json:"sourceText"`
	Candidates     []DirectorPromptCandidate `json:"candidates"`
	RecommendedKey string                    `json:"recommendedKey"`
	SelectedKey    string                    `json:"selectedKey,omitempty"`
	Status         string                    `json:"status"`
	CreatedAt      time.Time                 `json:"createdAt"`
	SelectedAt     *time.Time                `json:"selectedAt,omitempty"`
}

type CreateDirectorPromptProposalRequest struct {
	ProjectID  string `json:"projectId"`
	SourceText string `json:"sourceText"`
}

type SelectDirectorPromptProposalRequest struct {
	CandidateKey string `json:"candidateKey"`
}

func (s *Service) CreateDirectorPromptProposal(userID string, req CreateDirectorPromptProposalRequest) (DirectorPromptProposalSummary, error) {
	source := strings.TrimSpace(req.SourceText)
	if source == "" {
		return DirectorPromptProposalSummary{}, BadAuthRequest("创作内容不能为空")
	}
	if utf8.RuneCountInString(source) > 20000 {
		return DirectorPromptProposalSummary{}, BadAuthRequest("创作内容最多 20000 字")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID != "" {
		if err := s.ensureTaskProjectActive(userID, projectID); err != nil {
			return DirectorPromptProposalSummary{}, err
		}
	}
	recommended := directorRecommendation(source)
	candidates := directorCandidates(source, recommended)
	encoded, _ := json.Marshal(candidates)
	now := time.Now()
	proposal := model.DirectorPromptProposal{
		ID: newID(), UserID: userID, ProjectID: projectID, SourceText: source,
		CandidatesJSON: string(encoded), RecommendedKey: recommended,
		Status: "awaiting_selection", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(&proposal); err != nil {
		return DirectorPromptProposalSummary{}, err
	}
	return directorProposalSummary(proposal), nil
}

func (s *Service) SelectDirectorPromptProposal(userID string, id string, req SelectDirectorPromptProposalRequest) (DirectorPromptProposalSummary, error) {
	proposal, err := s.repo.DirectorPromptProposalForUser(userID, strings.TrimSpace(id))
	if err != nil {
		return DirectorPromptProposalSummary{}, err
	}
	key := strings.TrimSpace(req.CandidateKey)
	if !directorProposalHasCandidate(*proposal, key) {
		return DirectorPromptProposalSummary{}, BadAuthRequest("导演方案不存在")
	}
	selected, err := s.repo.SelectDirectorPromptProposal(userID, proposal.ID, key, time.Now())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DirectorPromptProposalSummary{}, BadAuthRequest("导演方案已选择或已失效")
		}
		return DirectorPromptProposalSummary{}, err
	}
	return directorProposalSummary(*selected), nil
}

func (s *Service) selectedDirectorPrompt(userID string, proposalID string, projectID string, sourceText string) (*model.DirectorPromptProposal, string, error) {
	if strings.TrimSpace(proposalID) == "" {
		return nil, "", BadAuthRequest("请先比较并选择一个专业导演扩写方案")
	}
	proposal, err := s.repo.DirectorPromptProposalForUser(userID, strings.TrimSpace(proposalID))
	if err != nil {
		return nil, "", BadAuthRequest("导演扩写方案不存在")
	}
	if proposal.Status != "selected" || proposal.SelectedKey == "" || proposal.ConsumedSessionID != "" {
		return nil, "", BadAuthRequest("导演扩写方案尚未选择或已被使用")
	}
	if strings.TrimSpace(proposal.ProjectID) != strings.TrimSpace(projectID) || strings.TrimSpace(proposal.SourceText) != strings.TrimSpace(sourceText) {
		return nil, "", BadAuthRequest("导演扩写方案与当前项目或原始内容不匹配")
	}
	for _, candidate := range directorProposalCandidates(*proposal) {
		if candidate.Key == proposal.SelectedKey {
			return proposal, candidate.Prompt, nil
		}
	}
	return nil, "", BadAuthRequest("已选导演扩写方案内容无效")
}

func directorRecommendation(source string) string {
	dialogueMarkers := strings.Count(source, "“") + strings.Count(source, "\"") + strings.Count(source, "：")
	actionMarkers := 0
	for _, marker := range []string{"走", "跑", "冲", "推", "拉", "转身", "抬头", "看向", "镜头", "动作"} {
		actionMarkers += strings.Count(source, marker)
	}
	if actionMarkers > dialogueMarkers {
		return "visual"
	}
	return "narrative"
}

func directorCandidates(source string, recommended string) []DirectorPromptCandidate {
	base := "\n\n原始创作内容：\n" + source + "\n\n交付要求：输出可直接生产的九列表格：镜号、时长、景别/机位、画面主体、可见动作与反应、台词/旁白、运镜、声音、首帧与视频生成提示词。镜号从 0 连续编号；时间节拍必须连续；每镜只保留一个主要运镜；角色身份、服装、道具、空间方向与光线必须跨镜连续；所有动作必须可见且带可观察反应；参考图覆盖不足时明确标注，不得假装已有素材。"
	return []DirectorPromptCandidate{
		{Key: "narrative", Name: "叙事优先", Summary: "先锁定人物动机、冲突升级和情绪转折，再把叙事节拍拆为可执行镜头。", Recommended: recommended == "narrative", Prompt: "你是专业短片叙事导演。保留原意，不补写无依据的核心剧情。先提炼人物目标、阻力、转折与结局，再按可拍摄的因果链拆镜；对白必须推动关系或行动，情绪变化必须通过表演、构图和声音被观察到。" + base},
		{Key: "visual", Name: "视听优先", Summary: "先设计构图、调度、动作节奏与声音桥，再校验剧情和连续性。", Recommended: recommended == "visual", Prompt: "你是专业视听导演。保留原意，不用空泛氛围词替代可拍动作。先设计空间关系、主体调度、镜头尺度、光线变化、声音桥和剪辑点，再校验人物动机与剧情信息完整；画面提示词描述首帧，视频提示词描述可见的时间变化。" + base},
	}
}

func directorProposalCandidates(proposal model.DirectorPromptProposal) []DirectorPromptCandidate {
	var candidates []DirectorPromptCandidate
	_ = json.Unmarshal([]byte(proposal.CandidatesJSON), &candidates)
	return candidates
}

func directorProposalHasCandidate(proposal model.DirectorPromptProposal, key string) bool {
	for _, candidate := range directorProposalCandidates(proposal) {
		if candidate.Key == key {
			return true
		}
	}
	return false
}

func directorProposalSummary(proposal model.DirectorPromptProposal) DirectorPromptProposalSummary {
	return DirectorPromptProposalSummary{ID: proposal.ID, ProjectID: proposal.ProjectID, SourceText: proposal.SourceText, Candidates: directorProposalCandidates(proposal), RecommendedKey: proposal.RecommendedKey, SelectedKey: proposal.SelectedKey, Status: proposal.Status, CreatedAt: proposal.CreatedAt, SelectedAt: proposal.SelectedAt}
}
