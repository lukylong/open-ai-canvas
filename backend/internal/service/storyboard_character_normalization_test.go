package service

import "testing"

func TestParseAgentStoryboardPlanNormalizesNamedObjects(t *testing.T) {
	raw := `{
		"title":"旧照片",
		"logline":"店员归还顾客遗失的旧照片。",
		"styleGuide":"都市暖色真人实拍",
		"characters":[{"name":"年轻店员"},{"characterName":"顾客"},"照片中的母亲"],
		"locations":[{"name":"咖啡店"},{"locationName":"街边"}],
		"shots":[{
			"title":"发现照片","description":"店员拾起照片。","durationSeconds":5,"dialogue":"",
			"characterIds":[],"narrativeIntent":"发现线索","viewerPOV":"跟随店员","performanceBlocking":"弯腰拾起",
			"shotSize":"中景","emotion":"疑惑","lightingAndAtmosphere":"午后自然光","audioEffects":"咖啡机声",
			"visualPrompt":"店员在桌边拾起旧照片","videoPrompt":"店员弯腰拾起照片后看向门口",
			"camera":"平视","motion":"固定","timeBeats":["0-2秒：拾起照片","2-5秒：看向门口"],"mustHave":["拾起照片"],
			"optionalDetails":[],"continuityOut":"照片留在手中","negativePrompt":"乱码","assetRefs":[]
		}]
	}`

	plan, err := parseAgentStoryboardPlan(raw)
	if err != nil {
		t.Fatalf("parseAgentStoryboardPlan() error = %v", err)
	}
	if got := plan.Characters; len(got) != 3 || got[0] != "年轻店员" || got[1] != "顾客" || got[2] != "照片中的母亲" {
		t.Fatalf("characters = %#v", got)
	}
	if got := plan.Locations; len(got) != 2 || got[0] != "咖啡店" || got[1] != "街边" {
		t.Fatalf("locations = %#v", got)
	}
	if got := plan.Shots[0].TimeBeats; got != "0-2秒：拾起照片；2-5秒：看向门口" {
		t.Fatalf("timeBeats = %q", got)
	}
}

func TestNormalizeStoryboardCharacterIDsWithoutConfiguredCharacters(t *testing.T) {
	plan := agentStoryboardPlan{Shots: []agentStoryboardShot{
		{CharacterIDs: []string{"character_linmo_v1", " character_linmo_v1 ", ""}},
		{CharacterIDs: []string{"character_customer_v1"}},
	}}

	removed := normalizeStoryboardCharacterIDs(&plan, nil)
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	for index, shot := range plan.Shots {
		if len(shot.CharacterIDs) != 0 {
			t.Fatalf("shot %d characterIds = %#v, want empty", index+1, shot.CharacterIDs)
		}
		if shot.CharacterIDs == nil {
			t.Fatalf("shot %d characterIds is nil, want []", index+1)
		}
	}
}

func TestNormalizeStoryboardCharacterIDsKeepsStrictValidationWithConfiguredCharacters(t *testing.T) {
	plan := agentStoryboardPlan{Shots: []agentStoryboardShot{{CharacterIDs: []string{"known", "invented"}}}}
	characters := []storyboardCharacterCard{{AssetID: "known", VersionID: "v1", Name: "已配置角色"}}

	removed := normalizeStoryboardCharacterIDs(&plan, characters)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if err := validateStoryboardCharacterIDs(plan, characters); err == nil {
		t.Fatal("configured-character validation unexpectedly accepted an invented assetId")
	}
}

func TestNormalizeStoryboardAssetRefsWithoutCanvasAssets(t *testing.T) {
	plan := agentStoryboardPlan{Shots: []agentStoryboardShot{
		{AssetRefs: []storyboardAssetRef{{NodeID: "char-linmo", Role: "character", Priority: 100}}},
		{AssetRefs: []storyboardAssetRef{{NodeID: "prop-photo", Role: "prop", Priority: 80}}},
	}}

	removed := normalizeStoryboardAssetRefs(&plan, nil)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	for index, shot := range plan.Shots {
		if len(shot.AssetRefs) != 0 {
			t.Fatalf("shot %d assetRefs = %#v, want empty", index+1, shot.AssetRefs)
		}
		if shot.AssetRefs == nil {
			t.Fatalf("shot %d assetRefs is nil, want []", index+1)
		}
	}
}

func TestNormalizeStoryboardAssetRefsKeepsStrictValidationWithCanvasAssets(t *testing.T) {
	plan := agentStoryboardPlan{Shots: []agentStoryboardShot{{AssetRefs: []storyboardAssetRef{{NodeID: "known", Role: "prop", Priority: 80}, {NodeID: "invented", Role: "prop", Priority: 80}}}}}
	assets := []storyboardAsset{{ID: "known", Title: "已配置素材"}}

	removed := normalizeStoryboardAssetRefs(&plan, assets)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if err := validateStoryboardAssetRefs(plan, assets); err == nil {
		t.Fatal("configured-asset validation unexpectedly accepted an invented nodeId")
	}
}
