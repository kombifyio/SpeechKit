package main

import (
	"encoding/json"
)

type queryInput struct {
	Query string `json:"query" jsonschema:"search query"`
}

type endpointInput struct {
	Path string `json:"path" jsonschema:"HTTP path such as /v1/dictation/transcribe"`
}

type integrationInput struct {
	Language string `json:"language" jsonschema:"go, python, typescript, or curl"`
	Mode     string `json:"mode" jsonschema:"dictation, assist, or voiceagent"`
}

type idInput struct {
	ID string `json:"id"`
}

type providerListInput struct {
	Mode string `json:"mode,omitempty" jsonschema:"dictation, assist, or voiceagent"`
}

type numericIDInput struct {
	ID int64 `json:"id"`
}

type payloadInput struct {
	ID      string         `json:"id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type transcriptListInput struct {
	Limit int `json:"limit,omitempty"`
}

type vocabularyInput struct {
	Language string           `json:"language,omitempty"`
	Entries  []map[string]any `json:"entries"`
}

type transcribeInput struct {
	AudioPath string `json:"audio_path"`
	Language  string `json:"language,omitempty"`
	Model     string `json:"model,omitempty"`
}

type ttsInput struct {
	Text   string  `json:"text"`
	Voice  string  `json:"voice,omitempty"`
	Locale string  `json:"locale,omitempty"`
	Format string  `json:"format,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
}

type configValidationInput struct {
	TOML string `json:"toml_snippet"`
}

type jsonValidationInput struct {
	Endpoint    string          `json:"endpoint"`
	Method      string          `json:"method,omitempty"`
	StatusCode  int             `json:"status_code,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

type codeInput struct {
	ClientCode string `json:"client_code"`
}

type changesInput struct {
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
}

type installPlanInput struct {
	Channel    string `json:"channel,omitempty" jsonschema:"stable or preview"`
	InstallDir string `json:"install_dir,omitempty" jsonschema:"default /opt/speechkit"`
	PublicBind bool   `json:"public_bind,omitempty" jsonschema:"whether to expose port 8080 on all interfaces"`
}

type selfCheckInput struct {
	ServerURL string `json:"server_url,omitempty" jsonschema:"base URL of the SpeechKit Server"`
}

type scaffoldInput struct {
	Template string            `json:"template,omitempty" jsonschema:"template name, default browser-dictation-react"`
	Vars     map[string]string `json:"vars,omitempty" jsonschema:"template variables such as APP_NAME and SPEECHKIT_SERVER_URL"`
}
