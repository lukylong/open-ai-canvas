package service

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type PersonalSeriesState struct {
	Series      []model.PersonalAssetSeries     `json:"series"`
	Memberships []model.PersonalAssetMembership `json:"memberships"`
}

func (s *Service) PersonalSeriesState(user *model.User) (*PersonalSeriesState, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	rows, err := s.repo.PersonalSeries(user.ID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.PersonalMemberships(user.ID)
	if err != nil {
		return nil, err
	}
	return &PersonalSeriesState{Series: rows, Memberships: members}, nil
}

// The entire tree is validated inside the account transaction, so moves cannot
// create cycles or exceed the shared library's eight-level hierarchy limit.
func validatePersonalSeriesTree(rows []model.PersonalAssetSeries) error {
	byID := map[string]model.PersonalAssetSeries{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	for _, row := range rows {
		seen := map[string]bool{}
		for id := row.ID; id != ""; {
			if seen[id] {
				return BadAuthRequest("不能移动到当前系列或其子系列")
			}
			seen[id] = true
			if len(seen) > sharedSeriesMaxDepth {
				return BadAuthRequest("系列最多支持 8 级")
			}
			parent, ok := byID[id]
			if !ok {
				return NotFound("上级系列不存在或无权访问")
			}
			id = parent.ParentID
		}
	}
	return nil
}

func personalSeriesContains(rows []model.PersonalAssetSeries, root, child string) bool {
	byID := map[string]string{}
	for _, row := range rows {
		byID[row.ID] = row.ParentID
	}
	for depth := 0; child != "" && depth < sharedSeriesMaxDepth; depth++ {
		if child == root {
			return true
		}
		child = byID[child]
	}
	return false
}

func clearPersonalSeriesCovers(repo *repository.Repository, userID string) error {
	rows, err := repo.PersonalSeries(userID)
	if err != nil {
		return err
	}
	members, err := repo.PersonalMemberships(userID)
	if err != nil {
		return err
	}
	byAsset := map[string]string{}
	for _, member := range members {
		byAsset[member.AssetID] = member.SeriesID
	}
	for _, row := range rows {
		if row.CoverAssetID != "" && !personalSeriesContains(rows, row.ID, byAsset[row.CoverAssetID]) {
			row.CoverAssetID = ""
			row.UpdatedAt = time.Now()
			if err := repo.SavePersonalSeries(&row); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) SavePersonalSeries(user *model.User, id, name, parentID string) (*model.PersonalAssetSeries, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	name, parentID = strings.TrimSpace(name), strings.TrimSpace(parentID)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return nil, BadAuthRequest("系列名称须为 1～80 个字符")
	}
	var result model.PersonalAssetSeries
	err := s.repo.PersonalSeriesTransaction(user.ID, func(repo *repository.Repository) error {
		rows, err := repo.PersonalSeries(user.ID)
		if err != nil {
			return err
		}
		now := time.Now()
		result = model.PersonalAssetSeries{ID: id, UserID: user.ID, CreatedAt: now}
		if id == "" {
			result.ID = newID()
		} else {
			found := false
			for _, row := range rows {
				if row.ID == id {
					result = row
					found = true
				}
			}
			if !found {
				return NotFound("系列不存在或无权访问")
			}
		}
		result.Name, result.ParentID, result.UpdatedAt = name, parentID, now
		next := []model.PersonalAssetSeries{result}
		for _, row := range rows {
			if row.ID == result.ID {
				continue
			}
			if row.ParentID == parentID && strings.EqualFold(row.Name, name) {
				return BadAuthRequest("同级系列名称已存在")
			}
			next = append(next, row)
		}
		if err := validatePersonalSeriesTree(next); err != nil {
			return err
		}
		if err := repo.SavePersonalSeries(&result); err != nil {
			return err
		}
		return clearPersonalSeriesCovers(repo, user.ID)
	})
	return &result, err
}

func (s *Service) DeletePersonalSeries(user *model.User, id string) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	return s.repo.PersonalSeriesTransaction(user.ID, func(repo *repository.Repository) error {
		rows, err := repo.PersonalSeries(user.ID)
		if err != nil {
			return err
		}
		found := false
		for _, row := range rows {
			if row.ID == id {
				found = true
			}
			if row.ParentID == id {
				return BadAuthRequest("请先移动或删除子系列；不会删除其中素材")
			}
		}
		if !found {
			return NotFound("系列不存在或无权访问")
		}
		if err := repo.RemovePersonalSeries(user.ID, id); err != nil {
			return err
		}
		return clearPersonalSeriesCovers(repo, user.ID)
	})
}

func (s *Service) MovePersonalAssets(user *model.User, seriesID string, assetIDs []string) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	assetIDs = uniqueDistributionAssetIDs(assetIDs)
	if len(assetIDs) == 0 || len(assetIDs) > 1000 {
		return BadAuthRequest("请选择 1～1000 个素材")
	}
	return s.repo.PersonalSeriesTransaction(user.ID, func(repo *repository.Repository) error {
		rows, err := repo.PersonalSeries(user.ID)
		if err != nil {
			return err
		}
		found := seriesID == ""
		for _, row := range rows {
			if row.ID == seriesID {
				found = true
			}
		}
		if !found {
			return NotFound("目标系列不存在或无权访问")
		}
		for _, id := range assetIDs {
			asset, err := repo.AssetForUser(user.ID, id)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NotFound("素材不存在或无权访问")
			}
			if err != nil {
				return err
			}
			if asset.Status == model.AssetVersionStatusArchived || asset.Kind == "entity" {
				return BadAuthRequest("不能移动已归档素材或实体")
			}
		}
		if err := repo.MovePersonalMembers(user.ID, seriesID, assetIDs); err != nil {
			return err
		}
		return clearPersonalSeriesCovers(repo, user.ID)
	})
}

func (s *Service) SetPersonalSeriesCover(user *model.User, id, assetID string) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	return s.repo.PersonalSeriesTransaction(user.ID, func(repo *repository.Repository) error {
		rows, err := repo.PersonalSeries(user.ID)
		if err != nil {
			return err
		}
		var target *model.PersonalAssetSeries
		for i := range rows {
			if rows[i].ID == id {
				target = &rows[i]
			}
		}
		if target == nil {
			return NotFound("系列不存在或无权访问")
		}
		if assetID != "" {
			asset, err := repo.AssetForUser(user.ID, assetID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NotFound("封面素材不存在或无权访问")
			}
			if err != nil {
				return err
			}
			if asset.Kind != "image" || asset.Status == model.AssetVersionStatusArchived {
				return BadAuthRequest("封面必须是有效图片")
			}
			membership, err := repo.PersonalSeriesForAsset(user.ID, assetID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return BadAuthRequest("封面必须位于当前系列或其子系列")
			}
			if err != nil {
				return err
			}
			if !personalSeriesContains(rows, id, membership.ID) {
				return BadAuthRequest("封面必须位于当前系列或其子系列")
			}
		}
		target.CoverAssetID, target.UpdatedAt = assetID, time.Now()
		return repo.SavePersonalSeries(target)
	})
}
