package service

import "testing"

func TestAppendResolvedVoiceRequestCarriesMigratedSampleAndReferenceText(t *testing.T) {
	body := map[string]interface{}{}
	input := canvasGenerationInput{
		ReferenceAudios: []providerMedia{{DataURL: "data:audio/wav;base64,UklGRg=="}},
		Metadata: map[string]interface{}{"resolvedCharacterVoice": map[string]interface{}{
			"profileId": "voice-1", "voiceKey": "zq-key", "provider": "zq-studio", "referenceText": "你好", "language": "zh", "timbre": "温暖",
		}},
	}
	appendResolvedVoiceRequest(body, input)
	if body["reference_audio"] != "data:audio/wav;base64,UklGRg==" || body["reference_text"] != "你好" {
		t.Fatalf("voice request = %#v", body)
	}
	profile, ok := body["voice_profile"].(map[string]interface{})
	if !ok || profile["source_id"] != "voice-1" || profile["voice_key"] != "zq-key" {
		t.Fatalf("voice profile = %#v", body["voice_profile"])
	}
}
