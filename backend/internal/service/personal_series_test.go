package service

import (
	"errors"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"reflect"
	"strings"
	"testing"
)

func personalSeriesFixture(t *testing.T) (*Service, *gorm.DB, *model.User) {
	t.Helper()
	svc, db := newSharedLibraryTestService(t)
	if err := db.AutoMigrate(&model.Asset{}, &model.PersonalAssetSeries{}, &model.PersonalAssetMembership{}); err != nil {
		t.Fatal(err)
	}
	u := &model.User{ID: "owner", Username: "owner", Status: model.UserStatusActive, Role: model.UserRoleUser}
	for _, row := range []any{u, &model.User{ID: "other", Username: "other", Status: model.UserStatusActive},
		&model.Asset{ID: "a", UserID: u.ID, Kind: "image", Title: "A", Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: "v-a", PayloadJSON: `{"metadata":{"taskId":"task-a","batch_id":"batch-original"},"data":{"storageKey":"resource:r-a"}}`},
		&model.Asset{ID: "b", UserID: u.ID, Kind: "image", Title: "B", Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: "v-b", PayloadJSON: `{"metadata":{"taskId":"task-b"}}`},
		&model.Asset{ID: "foreign", UserID: "other", Kind: "image", Status: model.AssetVersionStatusConfirmed},
		&model.Asset{ID: "archived", UserID: u.ID, Kind: "image", Status: model.AssetVersionStatusArchived},
		&model.Asset{ID: "video", UserID: u.ID, Kind: "video", Status: model.AssetVersionStatusConfirmed},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return svc, db, u
}

func TestPersonalSeriesLifecycleAndOwnership(t *testing.T) {
	svc, db, user := personalSeriesFixture(t)
	var before []model.Asset
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	root, err := svc.SavePersonalSeries(user, "", "我的系列", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.SavePersonalSeries(user, "", "子系列", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	other := &model.User{ID: "other", Role: model.UserRoleAdmin}
	foreignState, err := svc.PersonalSeriesState(other)
	if err != nil || len(foreignState.Series) != 0 || len(foreignState.Memberships) != 0 {
		t.Fatal("foreign account can read personal state", err)
	}
	if _, err := svc.PersonalSeriesState(nil); err == nil {
		t.Fatal("anonymous read allowed")
	}
	if _, err := svc.SavePersonalSeries(nil, "", "test", ""); err == nil {
		t.Fatal("anonymous create allowed")
	}
	if _, err := svc.SavePersonalSeries(other, root.ID, "stolen", ""); err == nil {
		t.Fatal("foreign update allowed")
	}
	if _, err := svc.SavePersonalSeries(other, "", "foreign child", root.ID); err == nil {
		t.Fatal("foreign parent allowed")
	}
	if err := svc.MovePersonalAssets(other, root.ID, []string{"foreign"}); err == nil {
		t.Fatal("foreign series allowed")
	}
	if err := svc.MovePersonalAssets(user, child.ID, []string{"a", "foreign"}); err == nil {
		t.Fatal("foreign member allowed")
	}
	state, _ := svc.PersonalSeriesState(user)
	if len(state.Memberships) != 0 {
		t.Fatal("failed mixed move partially saved")
	}
	if err := svc.MovePersonalAssets(user, child.ID, []string{"archived"}); err == nil {
		t.Fatal("archived member allowed")
	}
	if err := svc.MovePersonalAssets(user, child.ID, []string{"a", "b", "a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPersonalSeriesCover(user, root.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPersonalSeriesCover(user, root.ID, "video"); err == nil {
		t.Fatal("video cover allowed")
	}
	if err := svc.SetPersonalSeriesCover(other, root.ID, "foreign"); err == nil {
		t.Fatal("foreign cover allowed")
	}
	if err := svc.DeletePersonalSeries(user, root.ID); err == nil {
		t.Fatal("parent delete allowed")
	}
	if _, err := svc.SavePersonalSeries(user, root.ID, "cycle", child.ID); err == nil {
		t.Fatal("cycle allowed")
	}
	if _, err := svc.SavePersonalSeries(user, child.ID, "改名", ""); err != nil {
		t.Fatal(err)
	}
	state, _ = svc.PersonalSeriesState(user)
	for _, row := range state.Series {
		if row.ID == root.ID && row.CoverAssetID != "" {
			t.Fatal("parent cover not cleared after subtree move")
		}
	}
	if err := svc.SetPersonalSeriesCover(user, child.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.MovePersonalAssets(user, "", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	state, _ = svc.PersonalSeriesState(user)
	for _, row := range state.Series {
		if row.ID == child.ID && row.CoverAssetID != "" {
			t.Fatal("cover not cleared after moving image out")
		}
	}
	if err := svc.DeletePersonalSeries(other, child.ID); err == nil {
		t.Fatal("foreign deletion allowed")
	}
	if err := svc.DeletePersonalSeries(user, child.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePersonalSeries(user, root.ID); err != nil {
		t.Fatal(err)
	}
	state, _ = svc.PersonalSeriesState(user)
	if len(state.Series) != 0 {
		t.Fatalf("unexpected final state: %#v", state)
	}
	for _, member := range state.Memberships {
		if member.SeriesID != "" {
			t.Fatal("deleted series still has members")
		}
	}
	var after []model.Asset
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("management changed asset IDs, versions, payloads or timestamps")
	}
	t.Log("create/rename/tree move/member move/cover/delete passed; asset rows byte-equivalent")
}

func TestPersonalSeriesDepthAndNames(t *testing.T) {
	svc, _, user := personalSeriesFixture(t)
	for _, name := range []string{" ", strings.Repeat("长", 81)} {
		if _, err := svc.SavePersonalSeries(user, "", name, ""); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	first, err := svc.SavePersonalSeries(user, "", "Root", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SavePersonalSeries(user, "", " root ", ""); err == nil {
		t.Fatal("duplicate sibling accepted")
	}
	parent := first.ID
	for i := 2; i <= 8; i++ {
		row, err := svc.SavePersonalSeries(user, "", "child", parent)
		if err != nil {
			t.Fatal(err)
		}
		parent = row.ID
	}
	if _, err := svc.SavePersonalSeries(user, "", "ninth", parent); err == nil {
		t.Fatal("ninth level accepted")
	}
	second, err := svc.SavePersonalSeries(user, "", "Second", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SavePersonalSeries(user, first.ID, first.Name, second.ID); err == nil {
		t.Fatal("subtree moved past eight levels")
	}
}

func TestPersonalSeriesMoveRollsBackCoverAndMembership(t *testing.T) {
	svc, db, user := personalSeriesFixture(t)
	series, err := svc.SavePersonalSeries(user, "", "Group", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MovePersonalAssets(user, series.ID, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPersonalSeriesCover(user, series.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("fail_personal_cover", func(tx *gorm.DB) {
		if tx.Statement.Table == "personal_asset_series" {
			tx.AddError(errors.New("injected cover write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Update().Remove("fail_personal_cover") })
	if err := svc.MovePersonalAssets(user, "", []string{"a"}); err == nil {
		t.Fatal("expected rollback")
	}
	state, err := svc.PersonalSeriesState(user)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Memberships) != 1 || state.Memberships[0].SeriesID != series.ID || state.Series[0].CoverAssetID != "a" {
		t.Fatalf("partial state escaped transaction: %#v", state)
	}
}
