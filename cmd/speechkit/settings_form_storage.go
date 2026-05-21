package main

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	desktopsettings "github.com/kombifyio/SpeechKit/internal/desktop/settings"
)

func parseStoreSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	f.StoreSaveAudio = boolFormValue(req, "store_save_audio", cfg.Store.SaveAudio)
	f.StoreBackend = firstNonEmpty(trimmedFormValue(req, "store_backend"), cfg.Store.Backend, "sqlite")
	switch f.StoreBackend {
	case "sqlite", "postgres":
	default:
		return msgUnsupportedStore
	}
	f.StoreSQLitePath = valueOrDefault(trimmedFormValue(req, "store_sqlite_path"), cfg.Store.SQLitePath)
	f.StorePostgresDSN = valueOrDefault(trimmedFormValue(req, "store_postgres_dsn"), cfg.Store.PostgresDSN)
	if f.StoreBackend == "postgres" && f.StorePostgresDSN == "" {
		return msgPostgresDSNReq
	}
	f.StoreAudioRetention = nonNegativeIntFormValue(req, "store_audio_retention_days", cfg.Store.AudioRetentionDays)
	f.StoreMaxAudioStorage = nonNegativeIntFormValue(req, "store_max_audio_storage_mb", cfg.Store.MaxAudioStorageMB)
	return ""
}

func parseContentSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) {
	f.ModelDownloadDir = normalizeModelDownloadDir(req.FormValue("model_download_dir"))
	if !postFormIncludes(req, "model_download_dir") {
		f.ModelDownloadDir = strings.TrimSpace(cfg.General.ModelDownloadDir)
	}
	f.VocabularyDictionary = normalizeVocabularyDictionary(req.FormValue("vocabulary_dictionary"))
	if !postFormIncludes(req, "vocabulary_dictionary") {
		f.VocabularyDictionary = cfg.Vocabulary.Dictionary
	}
	f.Language = firstNonEmpty(trimmedFormValue(req, "language"), cfg.General.Language, "de")
	// dictate_silence_timeout_sec — see GeneralConfig.DictateSilenceTimeoutSec.
	// Treat absence of the form field as "keep current value." 0 explicitly
	// disables the watcher.
	if postFormIncludes(req, "dictate_silence_timeout_sec") {
		f.DictateSilenceTimeoutSec = nonNegativeIntFormValue(req, "dictate_silence_timeout_sec", cfg.General.DictateSilenceTimeoutSec)
	} else {
		f.DictateSilenceTimeoutSec = cfg.General.DictateSilenceTimeoutSec
	}
}

func normalizeVocabularyDictionary(input string) string {
	return desktopsettings.NormalizeMultiline(input)
}

func normalizeModelDownloadDir(input string) string {
	return strings.TrimSpace(input)
}
