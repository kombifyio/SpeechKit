package catalog_test

import (
	"fmt"
	"log"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

// ExampleCatalog_With extends the built-in catalog with a host-owned STT
// provider. The profile id follows the "<mode>.<provider>.<model>" shape, so
// the provider id, auth requirement and transport are derived; the result
// participates in mode listing, the provider matrix and policy filtering like
// any shipped provider.
func ExampleCatalog_With() {
	extended, err := catalog.DefaultCatalog().With(speechkit.ProviderProfile{
		ID:            "stt.acme.whisper-turbo",
		Name:          "Acme Whisper Turbo",
		Mode:          speechkit.ModeDictation,
		ProviderKind:  speechkit.ProviderKindCloudProvider,
		ExecutionMode: speechkit.ExecutionModeSelfHostedHTTP,
		Capabilities:  []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilitySTT},
	})
	if err != nil {
		log.Fatal(err)
	}

	profile, _ := extended.Profile("stt.acme.whisper-turbo")
	fmt.Println(profile.Provider, profile.AuthRequirement, profile.Transport)

	for _, row := range extended.ProviderMatrix() {
		if row.Provider == "acme" {
			fmt.Println(row.DisplayName, len(row.Profiles))
		}
	}
	// Output:
	// acme optional_api_key http
	// acme 1
}
