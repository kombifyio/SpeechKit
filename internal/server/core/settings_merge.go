//go:build linux

package core

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func mergeServerModelSettings(base, patch config.ServerModelSettings) config.ServerModelSettings {
	base.Version = 1
	if patch.OnboardingComplete {
		base.OnboardingComplete = true
	}
	if value := strings.TrimSpace(patch.OnboardingVersion); value != "" {
		base.OnboardingVersion = value
	}
	base.ServerAuth = mergeServerAuthSetting(base.ServerAuth, patch.ServerAuth)
	base.AdminAuth = mergeServerAdminAuthSetting(base.AdminAuth, patch.AdminAuth)
	base.Modes.Dictation = mergeServerModeSetting(base.Modes.Dictation, patch.Modes.Dictation)
	base.Modes.Assist = mergeServerModeSetting(base.Modes.Assist, patch.Modes.Assist)
	base.Modes.VoiceAgent = mergeServerModeSetting(base.Modes.VoiceAgent, patch.Modes.VoiceAgent)
	base.Credentials.OpenAI = mergeServerCredentialSetting(base.Credentials.OpenAI, patch.Credentials.OpenAI)
	base.Credentials.Groq = mergeServerCredentialSetting(base.Credentials.Groq, patch.Credentials.Groq)
	base.Credentials.Google = mergeServerCredentialSetting(base.Credentials.Google, patch.Credentials.Google)
	base.Credentials.Deepgram = mergeServerCredentialSetting(base.Credentials.Deepgram, patch.Credentials.Deepgram)
	base.Credentials.AssemblyAI = mergeServerCredentialSetting(base.Credentials.AssemblyAI, patch.Credentials.AssemblyAI)
	base.Credentials.HuggingFace = mergeServerCredentialSetting(base.Credentials.HuggingFace, patch.Credentials.HuggingFace)
	base.Credentials.OpenRouter = mergeServerCredentialSetting(base.Credentials.OpenRouter, patch.Credentials.OpenRouter)
	if patch.Dictation.Dictionary != nil {
		base.Dictation.Dictionary = patch.Dictation.Dictionary
	}
	if patch.Assist.EnabledTools != nil {
		base.Assist.EnabledTools = append([]string(nil), patch.Assist.EnabledTools...)
	}
	if patch.STT.Enabled != nil {
		base.STT.Enabled = patch.STT.Enabled
	}
	if value := strings.TrimSpace(patch.STT.URL); value != "" {
		base.STT.URL = value
	}
	if value := strings.TrimSpace(patch.STT.Model); value != "" {
		base.STT.Model = value
	}
	if patch.LLM.Enabled != nil {
		base.LLM.Enabled = patch.LLM.Enabled
	}
	if value := strings.TrimSpace(patch.LLM.BaseURL); value != "" {
		base.LLM.BaseURL = value
	}
	if value := strings.TrimSpace(patch.LLM.UtilityModel); value != "" {
		base.LLM.UtilityModel = value
	}
	if value := strings.TrimSpace(patch.LLM.AssistModel); value != "" {
		base.LLM.AssistModel = value
	}
	if value := strings.TrimSpace(patch.LLM.AgentModel); value != "" {
		base.LLM.AgentModel = value
	}
	if value := strings.TrimSpace(patch.LLM.HFRepo); value != "" {
		base.LLM.HFRepo = value
	}
	if value := strings.TrimSpace(patch.VoiceAgent.Provider); value != "" {
		base.VoiceAgent.Provider = strings.ToLower(value)
	}
	if value := strings.TrimSpace(patch.VoiceAgent.AgentProfileID); value != "" {
		base.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(value)
	}
	if patch.VoiceAgent.PromptTemplate != nil {
		base.VoiceAgent.PromptTemplate = patch.VoiceAgent.PromptTemplate
	}
	if patch.TTS.Enabled != nil {
		base.TTS.Enabled = patch.TTS.Enabled
	}
	return base
}

func mergeServerAdminAuthSetting(base, patch config.ServerAdminAuthSettings) config.ServerAdminAuthSettings {
	if patch.Enabled != nil {
		base.Enabled = patch.Enabled
	}
	if value := strings.TrimSpace(patch.Username); value != "" {
		base.Username = value
	}
	if value := strings.TrimSpace(patch.PasswordHash); value != "" {
		base.PasswordHash = value
	}
	if value := strings.TrimSpace(patch.PasswordValue); value != "" {
		base.PasswordValue = value
	}
	return base
}

func mergeServerAuthSetting(base, patch config.ServerAuthSettings) config.ServerAuthSettings {
	if value := strings.TrimSpace(patch.Mode); value != "" {
		base.Mode = strings.ToLower(value)
	}
	if value := strings.TrimSpace(patch.BearerTokenEnv); value != "" {
		base.BearerTokenEnv = value
	}
	if patch.GenerateToken != nil {
		base.GenerateToken = patch.GenerateToken
	}
	if value := strings.TrimSpace(patch.TokenValue); value != "" {
		base.TokenValue = value
	}
	return base
}

func shouldGenerateServerToken(auth config.ServerAuthSettings) bool {
	return strings.EqualFold(strings.TrimSpace(auth.Mode), config.ServerAuthModeManagedBearer) &&
		auth.GenerateToken != nil &&
		*auth.GenerateToken
}

func generateServerBearerToken() (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "sk-server-" + base64.RawURLEncoding.EncodeToString(random[:]), nil
}

func pairingServerURL(app *App, r *http.Request) string {
	var cfg *config.Config
	if app != nil {
		cfg = app.Cfg
	}
	if cfg != nil {
		for _, candidate := range []string{
			cfg.Server.PublicURL,
			cfg.Server.PublicBaseURL,
			cfg.Server.Discovery.AdvertiseURL,
		} {
			if u := strings.TrimRight(strings.TrimSpace(candidate), "/"); u != "" {
				return u
			}
		}
	}
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	scheme := "http"
	if serverRequestIsSecure(app, r) {
		scheme = "https"
	}
	return scheme + "://" + host
}

func pairingInstanceName(cfg *config.Config) string {
	if cfg != nil {
		if name := strings.TrimSpace(cfg.Server.Discovery.InstanceName); name != "" {
			return name
		}
	}
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(host)
}

func applyRuntimeServerAuth(app *App, auth config.ServerAuthSettings) {
	if app == nil || app.Cfg == nil {
		return
	}
	notes := config.ApplyServerAuthSettings(app.Cfg, auth)
	if len(notes) == 0 {
		return
	}
	if app.AuthState != nil {
		app.AuthState.Set(app.Cfg.Server.AuthMode, app.Cfg.Server.BearerTokenEnv, app.Cfg.Server.EdgeAuthSecretEnv)
	}
}

func applyRuntimeServerAdminAuth(app *App, auth config.ServerAdminAuthSettings) {
	if app == nil || app.Cfg == nil {
		return
	}
	notes := config.ApplyServerAdminAuthSettings(app.Cfg, auth)
	if len(notes) == 0 {
		return
	}
	if app.AuthState != nil {
		app.AuthState.SetAdmin(app.Cfg.Server.AdminUsername, app.Cfg.Server.AdminPasswordHash)
	}
}

func serverModelSettingsEqual(a, b config.ServerModelSettings) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func ensureSingleJSONValue(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("request body must contain a single JSON object")
	}
	return err
}

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func mergeServerModeSetting(base, patch config.ServerModeSetting) config.ServerModeSetting {
	if patch.Enabled != nil {
		base.Enabled = patch.Enabled
	}
	if value := strings.TrimSpace(patch.ProviderKind); value != "" {
		base.ProviderKind = value
	}
	if value := strings.TrimSpace(patch.ProfileID); value != "" {
		base.ProfileID = value
	}
	if value := strings.TrimSpace(patch.Model); value != "" {
		base.Model = value
	}
	return base
}

func mergeServerCredentialSetting(base, patch config.ServerProviderCredentialSettings) config.ServerProviderCredentialSettings {
	if patch.Enabled != nil {
		base.Enabled = patch.Enabled
	}
	if value := strings.TrimSpace(patch.Env); value != "" {
		base.Env = value
	}
	if value := strings.TrimSpace(patch.Value); value != "" {
		base.Value = value
	}
	return base
}
