package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"golang.org/x/crypto/bcrypt"
)

func ServerSettingsPath(cfg *Config) string {
	if path := strings.TrimSpace(os.Getenv(ServerSettingsPathEnv)); path != "" {
		return path
	}
	if cfg != nil && strings.TrimSpace(cfg.Store.SQLitePath) != "" {
		return filepath.Join(filepath.Dir(cfg.Store.SQLitePath), "server-settings.json")
	}
	return defaultServerSettingsPath
}

func LoadServerModelSettings(path string) (ServerModelSettings, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ServerModelSettings{}, false, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- server settings path is controlled by deployment config/env.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ServerModelSettings{}, false, nil
		}
		return ServerModelSettings{}, false, err
	}
	var settings ServerModelSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return ServerModelSettings{}, false, err
	}
	settings = NormalizeServerModelSettings(settings)
	return settings, true, nil
}

func SaveServerModelSettings(path string, settings ServerModelSettings) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("server settings path is empty")
	}
	settings = NormalizeServerModelSettings(settings)
	if settings.AdminAuth.PasswordValue != "" && settings.AdminAuth.Enabled == nil {
		enabled := true
		settings.AdminAuth.Enabled = &enabled
	}
	if err := validateServerModelSettings(settings); err != nil {
		return err
	}
	if value := settings.AdminAuth.PasswordValue; value != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		settings.AdminAuth.PasswordHash = string(hash)
		settings.AdminAuth.PasswordValue = ""
	}
	settings = SanitizeServerModelSettings(settings)
	settings.Version = 1
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create server settings dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write server settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace server settings: %w", err)
	}
	return nil
}

func SanitizeServerModelSettings(settings ServerModelSettings) ServerModelSettings {
	settings = NormalizeServerModelSettings(settings)
	settings.ServerAuth.GenerateToken = nil
	settings.ServerAuth.TokenValue = ""
	settings.AdminAuth.PasswordValue = ""
	settings.Credentials.OpenAI.Value = ""
	settings.Credentials.Groq.Value = ""
	settings.Credentials.Google.Value = ""
	settings.Credentials.Deepgram.Value = ""
	settings.Credentials.AssemblyAI.Value = ""
	settings.Credentials.HuggingFace.Value = ""
	settings.Credentials.OpenRouter.Value = ""
	return settings
}

func NormalizeServerModelSettings(settings ServerModelSettings) ServerModelSettings {
	settings.ServerAuth.Mode = strings.ToLower(strings.TrimSpace(settings.ServerAuth.Mode))
	settings.ServerAuth.BearerTokenEnv = strings.TrimSpace(settings.ServerAuth.BearerTokenEnv)
	settings.ServerAuth.TokenValue = strings.TrimSpace(settings.ServerAuth.TokenValue)
	settings.AdminAuth.Username = strings.TrimSpace(settings.AdminAuth.Username)
	settings.AdminAuth.PasswordHash = strings.TrimSpace(settings.AdminAuth.PasswordHash)
	settings.AdminAuth.PasswordValue = strings.TrimSpace(settings.AdminAuth.PasswordValue)
	settings.LLM.UtilityModel = normalizeServerModelValue(settings.LLM.UtilityModel)
	settings.LLM.AssistModel = normalizeServerModelValue(settings.LLM.AssistModel)
	settings.LLM.AgentModel = normalizeServerModelValue(settings.LLM.AgentModel)
	settings.Modes.Assist.Model = normalizeServerModelValue(settings.Modes.Assist.Model)
	settings.Modes.VoiceAgent.Model = normalizeServerModelValue(settings.Modes.VoiceAgent.Model)
	if settings.Dictation.Dictionary != nil {
		value := normalizeServerMultiline(*settings.Dictation.Dictionary)
		settings.Dictation.Dictionary = &value
	}
	if settings.VoiceAgent.PromptTemplate != nil {
		value := normalizeServerMultiline(*settings.VoiceAgent.PromptTemplate)
		settings.VoiceAgent.PromptTemplate = &value
	}
	if strings.TrimSpace(settings.VoiceAgent.AgentProfileID) != "" {
		settings.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(settings.VoiceAgent.AgentProfileID)
	}
	settings.Assist.EnabledTools = normalizeServerStringList(settings.Assist.EnabledTools)
	return settings
}

func normalizeServerModelValue(value string) string {
	value = strings.TrimSpace(value)
	if localLLMModelNeedsDefault(value) {
		return defaultServerLLMModel
	}
	return value
}
