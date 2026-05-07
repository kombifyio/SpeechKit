package store

import (
	"mime"
	"os"
	"path/filepath"
	"strings"
)

func buildLocalAudioAsset(path string, durationMs int64) *AudioAsset {
	if path == "" {
		return nil
	}

	asset := &AudioAsset{
		StorageKind: AudioStorageLocalFile,
		Path:        path,
		MimeType:    mimeTypeForAudioPath(path),
		DurationMs:  durationMs,
	}
	if info, err := os.Stat(path); err == nil {
		asset.SizeBytes = info.Size()
	}
	return asset
}

func normalizeAudioAssetInput(audio AudioAssetInput) AudioAssetInput {
	audio.MimeType = strings.TrimSpace(audio.MimeType)
	audio.Extension = strings.TrimSpace(strings.ToLower(audio.Extension))
	if audio.MimeType == "" {
		audio.MimeType = "audio/wav"
	}
	if audio.Extension == "" {
		audio.Extension = extensionForMimeType(audio.MimeType)
	}
	if !strings.HasPrefix(audio.Extension, ".") {
		audio.Extension = "." + audio.Extension
	}
	switch audio.Extension {
	case ".wav", ".mp3", ".ogg", ".opus", ".webm", ".pcm", ".raw":
	default:
		audio.Extension = ".audio"
	}
	return audio
}

func audioAssetInputFromBytes(data []byte) AudioAssetInput {
	return normalizeAudioAssetInput(AudioAssetInput{Data: data, MimeType: "audio/wav", Extension: ".wav"})
}

func mimeTypeForAudioPath(path string) string {
	if mt := strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))); mt != "" {
		if before, _, ok := strings.Cut(mt, ";"); ok {
			return strings.TrimSpace(before)
		}
		return mt
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".webm":
		return "audio/webm"
	case ".pcm", ".raw":
		return "audio/L16"
	default:
		return "audio/wav"
	}
}

func extensionForMimeType(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/webm":
		return ".webm"
	case "audio/l16", "audio/pcm":
		return ".pcm"
	default:
		return ".wav"
	}
}
