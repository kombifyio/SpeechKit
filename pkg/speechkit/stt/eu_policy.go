package stt

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func euDictationCandidates(ctx context.Context, candidates []STTProvider, profileID, model string) ([]STTProvider, error) {
	if !netsec.DataProfileEU(ctx) {
		return candidates, nil
	}
	if err := netsec.EUVoiceProfileError(ctx); err != nil {
		return nil, err
	}
	allowed := func(provider string) bool {
		id := catalog.NormalizeProviderID(provider)
		return id == "deepgram" || id == "assemblyai"
	}
	if (strings.TrimSpace(profileID) != "" && !allowed(profileID)) || strings.Contains(strings.ToLower(profileID+" "+model), "whisper") {
		return nil, fmt.Errorf("data_profile_processor_ineligible: selected dictation profile is not qualified for EU")
	}
	regional := make([]STTProvider, 0, len(candidates))
	for _, provider := range candidates {
		if allowed(provider.Name()) {
			regional = append(regional, provider)
		}
	}
	if len(regional) == 0 {
		return nil, fmt.Errorf("data_profile_processor_ineligible: no EU dictation provider is configured")
	}
	return regional, nil
}
