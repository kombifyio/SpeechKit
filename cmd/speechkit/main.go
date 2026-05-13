package main

import (
	_ "github.com/kombifyio/SpeechKit/internal/kombify"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

var newHuggingFaceProvider = func(model, token string) stt.STTProvider {
	return stt.NewHuggingFaceProvider(model, token)
}

func main() {
	_, closeLogFile := initAppLogging()
	defer closeLogFile()

	runDesktopApp(closeLogFile)
}

// buildRouter, buildGenkitConfig, buildTTSRouter, validateCloudProviders,
// missingProviderHint, executableDir, defaultLocalModelPath, escapeJS, and
// runtimeConfigPath are in app_init.go.
