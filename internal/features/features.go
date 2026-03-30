// Package features provides runtime feature detection for UI gating.
// The frontend queries GET /features to determine what to show/hide.
package features

import (
	"github.com/kombifyio/SpeechKit/internal/auth"
	"github.com/kombifyio/SpeechKit/internal/config"
)

// Features describes what capabilities are available at runtime.
type Features struct {
	CloudMode     bool   `json:"cloudMode"`
	HasAuth       bool   `json:"hasAuth"`
	HasCloudStore bool   `json:"hasCloudStore"`
	LoggedIn      bool   `json:"loggedIn"`
	PlanName      string `json:"plan"`
	InstallMode   string `json:"installMode"`
}

// Detect determines available features based on install state and registered providers.
func Detect(installState *config.InstallState) Features {
	f := Features{
		InstallMode: string(installState.Mode),
	}

	if installState.Mode == config.InstallModeCloud {
		f.CloudMode = true
	}

	authProvider := auth.GetAuthProvider()
	if authProvider != nil {
		f.HasAuth = true
		f.LoggedIn = authProvider.IsLoggedIn()
	}

	return f
}
