package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestPersonalSeriesNestedDistribution(t *testing.T) {
	svc, db, user := personalSeriesFixture(t)
	t.Setenv("CANVAS_DISTRIBUTION_URL", "https://distribution.invalid/resources")
	t.Setenv("CANVAS_DISTRIBUTION_KEY_ID", "test")
	t.Setenv("CANVAS_DISTRIBUTION_SECRET", "test")
	if err := db.AutoMigrate(&model.AssetVersion{}, &model.AssetRepresentation{}, &model.DistributionPublication{}, &model.DistributionOutbox{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "video"} {
		resource := &model.Resource{ID: "r-" + id, UserID: user.ID, Kind: "image", Status: model.ResourceStatusReady, PublicURL: "https://example.invalid/" + id + ".png"}
		if err := db.Create(resource).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.Asset{}).Where("id = ?", id).Updates(map[string]any{"primary_version_id": "", "payload_json": `{"metadata":{"taskId":"task-` + id + `"},"data":{"storageKey":"resource:r-` + id + `"}}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	create := func(name, parent string) *model.PersonalAssetSeries {
		t.Helper()
		row, err := svc.SavePersonalSeries(user, "", name, parent)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	root := create("美食", "")
	middle := create("家常菜", root.ID)
	leaf := create("蒜蓉虾", middle.ID)
	otherRoot := create("宴席", "")
	sameName := create("蒜蓉虾", otherRoot.ID)
	for id, series := range map[string]string{"a": leaf.ID, "b": sameName.ID, "video": root.ID} {
		if err := svc.MovePersonalAssets(user, series, []string{id}); err != nil {
			t.Fatal(err)
		}
	}
	parentHints := map[string]any{"series_id": root.ID, "series_label": root.Name, "series_type": "manual", "series_path": "伪造路径", "series_parent_id": "foreign", "series_path_ids": []string{"foreign"}}
	result, err := svc.CreateDistributionPublications(user, CreateDistributionPublicationsRequest{AssetIDs: []string{"a", "b", "video"}, Metadata: parentHints})
	if err != nil || result.AcceptedCount != 3 {
		t.Fatalf("batch=%#v err=%v", result, err)
	}
	read := func(pub *model.DistributionPublication) distributionResource {
		t.Helper()
		var event distributionEvent
		if err := json.Unmarshal([]byte(pub.PayloadJSON), &event); err != nil {
			t.Fatal(err)
		}
		return event.Resources[0]
	}
	a, b, parent := read(result.Items[0].Publication), read(result.Items[1].Publication), read(result.Items[2].Publication)
	t.Logf("NESTED: leaf=%v path=%v; same-name=%v path=%v; parent-direct=%v", a.Metadata["series_label"], a.Metadata["series_path"], b.Metadata["series_label"], b.Metadata["series_path"], parent.Metadata["series_label"])
	if a.Metadata["series_label"] != "蒜蓉虾" || b.Metadata["series_label"] != "蒜蓉虾" || parent.Metadata["series_label"] != "美食" {
		t.Fatal("parent request overrode actual member name")
	}
	if a.Metadata["series_id"] == b.Metadata["series_id"] {
		t.Fatal("same-name series under different parents merged")
	}
	if a.Metadata["series_path"] != "美食 / 家常菜 / 蒜蓉虾" || a.Metadata["series_parent_id"] != middle.ID || !reflect.DeepEqual(a.Metadata["series_path_ids"], []any{root.ID, middle.ID, leaf.ID}) {
		t.Fatal("hierarchy metadata missing or request-controlled")
	}
	if a.Metadata["generation_task_id"] != "task-a" {
		t.Fatal("generation lineage lost")
	}
	receiverCases, err := json.Marshal([]distributionResource{a, b, parent})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("RECEIVER_CASES=" + string(receiverCases))

	// A parent rename affects the path, not the member's own display label.
	if _, err := svc.SavePersonalSeries(user, middle.ID, "热菜", root.ID); err != nil {
		t.Fatal(err)
	}
	renamed, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{Metadata: parentHints})
	if err != nil {
		t.Fatal(err)
	}
	updated := read(renamed)
	if renamed.ID == result.Items[0].Publication.ID || updated.Version <= a.Version || updated.Metadata["series_label"] != "蒜蓉虾" || updated.Metadata["series_path"] != "美食 / 热菜 / 蒜蓉虾" {
		t.Fatal("ancestor rename reused stale version or changed leaf label")
	}
	if _, err := svc.SavePersonalSeries(user, middle.ID, "热菜", otherRoot.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	updatedMoved := read(moved)
	if updatedMoved.Version <= updated.Version || updatedMoved.Metadata["series_path"] != "宴席 / 热菜 / 蒜蓉虾" {
		t.Fatal("ancestor move did not update path/version")
	}
	if _, err := svc.SavePersonalSeries(user, leaf.ID, "白灼虾", middle.ID); err != nil {
		t.Fatal(err)
	}
	renamedLeaf, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(renamedLeaf); got.Metadata["series_label"] != "白灼虾" || got.Version <= updatedMoved.Version {
		t.Fatal("leaf rename not reflected")
	}
	if err := svc.MovePersonalAssets(user, "", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	unassigned, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{Metadata: parentHints})
	if err != nil {
		t.Fatal(err)
	}
	cleared := read(unassigned)
	if cleared.Metadata["series_id"] != "task-a" || cleared.Metadata["series_path"] != nil || cleared.Metadata["series_path_ids"] != nil || cleared.Metadata["series_parent_id"] != nil {
		t.Fatal("move-out retained stale manual hierarchy")
	}
	t.Log("ancestor rename/move, leaf rename and move-out all preserve the correct direct-series label and advance explicit-sync versions")

	deep := create("第一级", "")
	for i := 2; i <= 8; i++ {
		deep = create("下一级", deep.ID)
	}
	if err := svc.MovePersonalAssets(user, deep.ID, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	deepPub, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if ids, ok := read(deepPub).Metadata["series_path_ids"].([]any); !ok || len(ids) != 8 {
		t.Fatal("eight-level hierarchy not preserved")
	}
	var beforeCount int64
	if err := db.Model(&model.DistributionPublication{}).Count(&beforeCount).Error; err != nil {
		t.Fatal(err)
	}
	foreign, err := svc.SavePersonalSeries(&model.User{ID: "other"}, "", "其他账号的父系列", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, badParent := range []string{deep.ID, "missing-parent", foreign.ID} {
		if err := db.Model(&model.PersonalAssetSeries{}).Where("id = ?", deep.ID).Update("parent_id", badParent).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateDistributionPublication(user, "a", CreateDistributionPublicationRequest{}); err == nil {
			t.Fatal("corrupt or foreign ancestor accepted")
		}
	}
	var afterCount int64
	if err := db.Model(&model.DistributionPublication{}).Count(&afterCount).Error; err != nil {
		t.Fatal(err)
	}
	if beforeCount != afterCount {
		t.Fatal("invalid hierarchy enqueued a publication")
	}
	t.Log("eight levels supported; cycles, missing parents and foreign-account parents rejected without enqueueing")
}
