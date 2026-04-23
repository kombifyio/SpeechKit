package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/localllm"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func apiV1ReadinessForCatalog(ctx context.Context, cfg *config.Config, catalog models.Catalog, sttRouter *router.Router) []speechkit.Readiness {
	readiness := make([]speechkit.Readiness, 0, len(catalog.Profiles))
	active := activeProfilesFromConfig(cfg, catalog)
	downloadCatalog := downloads.CatalogWithStatus(ctx, cfg, downloads.ReadinessStatusOptions)
	for _, profile := range catalog.Profiles {
		mode := apiV1ModeForModality(profile.Modality)
		if mode == "" {
			continue
		}
		readiness = append(readiness, apiV1ProfileReadiness(ctx, cfg, sttRouter, active, downloadCatalog, mode, profile))
	}
	return readiness
}

func apiV1ProfileReadiness(ctx context.Context, cfg *config.Config, sttRouter *router.Router, active map[string]string, downloadCatalog []downloads.Item, mode string, profile models.Profile) speechkit.Readiness {
	isActive := active[string(profile.Modality)] == profile.ID
	result := speechkit.Readiness{
		SchemaVersion:    speechkit.ReadinessSchemaVersion,
		ProfileID:        profile.ID,
		Mode:             speechkit.NormalizeMode(speechkit.Mode(mode)),
		ProviderKind:     speechkit.ProviderKind(profile.ProviderKind),
		ExecutionMode:    speechkit.ExecutionMode(profile.ExecutionMode),
		ModelID:          selectedModelIDForProfile(cfg, profile),
		Source:           profile.Source,
		Active:           isActive,
		Default:          profile.Default,
		Configured:       isActive || profileMatchesConfig(cfg, profile),
		CredentialsReady: true,
		CapabilityReady:  true,
		RuntimeReady:     true,
		Artifacts:        apiV1ReadinessArtifacts(downloadCatalog, profile.ID),
	}
	publicProfile := speechkit.ProviderProfile{
		ID:           profile.ID,
		Mode:         result.Mode,
		ProviderKind: speechkit.ProviderKind(profile.ProviderKind),
		Capabilities: apiV1Capabilities(profile.Capabilities),
	}
	if err := speechkit.ValidateProfileForMode(publicProfile, result.Mode); err != nil {
		result.CapabilityReady = false
		apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
			ID:       "capability.mode_contract",
			Label:    "Mode contract",
			Category: "capability",
			Required: true,
			Ready:    false,
			Missing:  err.Error(),
		})
	} else {
		apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
			ID:       "capability.mode_contract",
			Label:    "Mode contract",
			Category: "capability",
			Required: true,
			Ready:    true,
		})
	}

	result.CredentialsReady = profileCredentialAvailable(cfg, profile)
	credentialMissing := ""
	if !result.CredentialsReady {
		credentialMissing = apiV1CredentialMissingMessage(profile)
		apiV1AddAction(&result, speechkit.ReadinessAction{
			ID:     "configure_credential",
			Label:  "Configure provider credential",
			Kind:   "configure_credential",
			Target: apiV1CredentialTarget(profile),
		})
	}
	apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
		ID:       "credential.provider",
		Label:    "Provider credential",
		Category: "credential",
		Required: apiV1ProfileRequiresCredential(profile),
		Ready:    result.CredentialsReady,
		Missing:  credentialMissing,
	})

	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		if profile.Modality == models.ModalitySTT {
			apiV1CheckLocalSTTReadiness(ctx, cfg, sttRouter, &result)
		} else {
			apiV1CheckLocalLLMReadiness(ctx, cfg, profile, &result)
		}
	case models.ExecutionModeOllama:
		baseURLReady := strings.TrimSpace(cfg.Providers.Ollama.BaseURL) != ""
		if !baseURLReady {
			result.RuntimeReady = false
			apiV1AddAction(&result, speechkit.ReadinessAction{
				ID:     "configure_ollama",
				Label:  "Configure Ollama base URL",
				Kind:   "configure_provider",
				Target: "ollama",
			})
		}
		apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
			ID:       "runtime.ollama_base_url",
			Label:    "Ollama base URL",
			Category: "runtime",
			Required: true,
			Ready:    baseURLReady,
			Missing:  apiV1MissingWhen(!baseURLReady, "ollama base URL missing"),
		})
		apiV1AddArtifactActions(&result)
	case models.ExecutionModeHFRouted:
		buildReady := config.ManagedHuggingFaceAvailableInBuild()
		if !buildReady {
			result.RuntimeReady = false
		}
		apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
			ID:       "runtime.huggingface_build",
			Label:    "Hugging Face build support",
			Category: "runtime",
			Required: true,
			Ready:    buildReady,
			Missing:  apiV1MissingWhen(!buildReady, "hugging face unavailable in this build"),
		})
	default:
		apiV1AddRequirement(&result, speechkit.ReadinessRequirement{
			ID:       "runtime.provider",
			Label:    "Provider runtime",
			Category: "runtime",
			Required: true,
			Ready:    true,
		})
	}
	result.Ready = result.CredentialsReady && result.RuntimeReady && result.CapabilityReady
	return result
}

func apiV1CheckLocalSTTReadiness(ctx context.Context, cfg *config.Config, sttRouter *router.Router, result *speechkit.Readiness) {
	if cfg == nil {
		result.RuntimeReady = false
		apiV1AddRequirement(result, speechkit.ReadinessRequirement{
			ID:       "runtime.config",
			Label:    "Runtime config",
			Category: "runtime",
			Required: true,
			Ready:    false,
			Missing:  "config unavailable",
		})
		return
	}
	modelPath := strings.TrimSpace(configuredLocalSTTModelPath(cfg))
	status := stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU).VerifyInstallation()
	if !status.BinaryFound {
		result.RuntimeReady = false
		apiV1AddAction(result, speechkit.ReadinessAction{
			ID:     "install_local_stt_runtime",
			Label:  "Install local STT runtime",
			Kind:   "install_runtime",
			Target: "whisper.cpp",
		})
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "runtime.whisper_binary",
		Label:    "whisper.cpp server binary",
		Category: "runtime",
		Required: true,
		Ready:    status.BinaryFound,
		Missing:  apiV1MissingWhen(!status.BinaryFound, "whisper-server binary missing"),
	})

	modelSelected := modelPath != ""
	if !modelSelected {
		result.RuntimeReady = false
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "model.local_stt_selected",
		Label:    "Local transcription model selected",
		Category: "model",
		Required: true,
		Ready:    modelSelected,
		Missing:  apiV1MissingWhen(!modelSelected, "local transcription model path missing"),
	})

	modelFound := modelSelected && status.ModelFound
	if !modelFound {
		result.RuntimeReady = false
		apiV1AddArtifactActions(result)
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "model.local_stt_file",
		Label:    "Local transcription model file",
		Category: "model",
		Required: true,
		Ready:    modelFound,
		Missing:  apiV1MissingWhen(!modelFound, "local transcription model missing"),
	})

	if sttRouter != nil && sttRouter.Local() != nil && !providerReady(ctx, sttRouter.Local()) {
		result.RuntimeReady = false
		apiV1AddRequirement(result, speechkit.ReadinessRequirement{
			ID:       "runtime.local_stt_ready",
			Label:    "Local transcription runtime ready",
			Category: "runtime",
			Required: true,
			Ready:    false,
			Missing:  "local transcription runtime not ready",
		})
	}
}

func apiV1CheckLocalLLMReadiness(ctx context.Context, cfg *config.Config, profile models.Profile, result *speechkit.Readiness) {
	if cfg == nil {
		result.RuntimeReady = false
		apiV1AddRequirement(result, speechkit.ReadinessRequirement{
			ID:       "runtime.config",
			Label:    "Runtime config",
			Category: "runtime",
			Required: true,
			Ready:    false,
			Missing:  "config unavailable",
		})
		return
	}

	modelPath := apiV1LocalLLMModelPath(cfg, profile)
	port := cfg.LocalLLM.Port
	if port == 0 {
		port = 8082
	}
	status := localllm.NewServer(port, modelPath, cfg.LocalLLM.GPU).VerifyInstallation()

	if !status.BinaryFound {
		result.RuntimeReady = false
		apiV1AddAction(result, speechkit.ReadinessAction{
			ID:     "install_local_llm_runtime",
			Label:  "Install local LLM runtime",
			Kind:   "install_runtime",
			Target: "llama.cpp",
		})
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "runtime.llama_binary",
		Label:    "llama.cpp server binary",
		Category: "runtime",
		Required: true,
		Ready:    status.BinaryFound,
		Missing:  apiV1MissingWhen(!status.BinaryFound, "llama-server binary missing"),
	})

	baseURL := strings.TrimSpace(cfg.LocalLLM.BaseURL)
	if baseURL == "" {
		baseURL = config.DefaultLocalLLMBaseURL
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "runtime.local_llm_base_url",
		Label:    "SpeechKit local LLM endpoint",
		Category: "runtime",
		Required: true,
		Ready:    true,
	})

	modelSelected := strings.TrimSpace(modelPath) != ""
	if !modelSelected {
		result.RuntimeReady = false
		apiV1AddAction(result, speechkit.ReadinessAction{
			ID:     "select_local_llm_model",
			Label:  "Select local LLM model",
			Kind:   "select_artifact",
			Target: profile.ID,
		})
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "model.local_llm_selected",
		Label:    "Local LLM model selected",
		Category: "model",
		Required: true,
		Ready:    modelSelected,
		Missing:  apiV1MissingWhen(!modelSelected, "local LLM model path missing"),
	})

	modelReady := modelSelected && status.ModelFound
	if !modelReady {
		result.RuntimeReady = false
		apiV1AddArtifactActions(result)
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "model.local_llm_file",
		Label:    "Local LLM model file",
		Category: "model",
		Required: true,
		Ready:    modelReady,
		Missing:  apiV1MissingWhen(!modelReady, "local LLM model missing"),
	})

	runtimeEndpointReady := false
	runtimeEndpointProblem := ""
	if status.BinaryFound && modelReady {
		if err := localllm.ProbeEndpoint(ctx, baseURL); err != nil {
			runtimeEndpointProblem = "local LLM runtime not ready"
			result.RuntimeReady = false
			apiV1AddAction(result, speechkit.ReadinessAction{
				ID:     "start_local_llm_runtime",
				Label:  "Start local LLM runtime",
				Kind:   "install_runtime",
				Target: "llama.cpp",
			})
		} else {
			runtimeEndpointReady = true
		}
	} else {
		runtimeEndpointProblem = "local LLM runtime waits for a selected GGUF model"
	}
	apiV1AddRequirement(result, speechkit.ReadinessRequirement{
		ID:       "runtime.local_llm_endpoint_ready",
		Label:    "Local LLM runtime reachable",
		Category: "runtime",
		Required: true,
		Ready:    runtimeEndpointReady,
		Missing:  apiV1MissingWhen(!runtimeEndpointReady, runtimeEndpointProblem),
	})
}

func apiV1LocalLLMModelPath(cfg *config.Config, profile models.Profile) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.LocalLLM.ModelPath); modelPath != "" {
		return modelPath
	}
	modelID := strings.TrimSpace(selectedModelIDForProfile(cfg, profile))
	if modelID == "" || strings.Contains(modelID, ":") || filepath.IsAbs(modelID) {
		return ""
	}
	return filepath.Join(downloads.ResolveLocalLLMModelsDir(cfg), filepath.Base(modelID))
}

func apiV1ReadinessArtifacts(downloadCatalog []downloads.Item, profileID string) []speechkit.ReadinessArtifact {
	var artifacts []speechkit.ReadinessArtifact
	for _, item := range downloadCatalog {
		if item.ProfileID != profileID {
			continue
		}
		artifacts = append(artifacts, speechkit.ReadinessArtifact{
			ID:             item.ID,
			Name:           item.Name,
			Kind:           string(item.Kind),
			SizeLabel:      item.SizeLabel,
			SizeBytes:      item.SizeBytes,
			Available:      item.Available,
			Selected:       item.Selected,
			RuntimeReady:   item.RuntimeReady,
			RuntimeProblem: item.RuntimeProblem,
			Recommended:    item.Recommended,
		})
	}
	return artifacts
}

func apiV1AddRequirement(result *speechkit.Readiness, requirement speechkit.ReadinessRequirement) {
	result.Requirements = append(result.Requirements, requirement)
	if requirement.Required && !requirement.Ready && strings.TrimSpace(requirement.Missing) != "" {
		result.Missing = append(result.Missing, requirement.Missing)
	}
}

func apiV1AddAction(result *speechkit.Readiness, action speechkit.ReadinessAction) {
	for _, existing := range result.Actions {
		if existing.ID == action.ID && existing.Target == action.Target {
			return
		}
	}
	result.Actions = append(result.Actions, action)
}

func apiV1AddArtifactActions(result *speechkit.Readiness) {
	for _, artifact := range result.Artifacts {
		if !artifact.Available {
			apiV1AddAction(result, speechkit.ReadinessAction{
				ID:     "download_" + artifact.ID,
				Label:  "Download " + artifact.Name,
				Kind:   "download_artifact",
				Target: artifact.ID,
			})
			continue
		}
		if !artifact.Selected {
			apiV1AddAction(result, speechkit.ReadinessAction{
				ID:     "select_" + artifact.ID,
				Label:  "Select " + artifact.Name,
				Kind:   "select_artifact",
				Target: artifact.ID,
			})
		}
	}
}

func apiV1ProfileRequiresCredential(profile models.Profile) bool {
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal, models.ExecutionModeOllama:
		return false
	default:
		return true
	}
}

func apiV1CredentialTarget(profile models.Profile) string {
	switch profile.ExecutionMode {
	case models.ExecutionModeHFRouted:
		return "huggingface"
	case models.ExecutionModeOpenAI:
		return "openai"
	case models.ExecutionModeGroq:
		return "groq"
	case models.ExecutionModeGoogle:
		return "google"
	case models.ExecutionModeOpenRouter:
		return "openrouter"
	default:
		return ""
	}
}

func apiV1CredentialMissingMessage(profile models.Profile) string {
	target := apiV1CredentialTarget(profile)
	if target == "" {
		return "credential not configured"
	}
	return target + " credential not configured"
}

func apiV1MissingWhen(condition bool, message string) string {
	if condition {
		return message
	}
	return ""
}

func apiV1Capabilities(input []models.Capability) []speechkit.Capability {
	out := make([]speechkit.Capability, 0, len(input))
	for _, capability := range input {
		out = append(out, speechkit.Capability(capability))
	}
	return out
}
