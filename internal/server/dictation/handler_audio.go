//go:build linux

package dictation

import (
	"mime"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/store"
)

func audioAssetInput(raw []byte, contentType, sourceFormat string, durationMs int64) store.AudioAssetInput {
	mimeType := strings.TrimSpace(contentType)
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}
	extension := audioExtension(mimeType, sourceFormat)
	if mimeType == "" {
		mimeType = audioMimeType(extension)
	}
	return store.AudioAssetInput{
		Data:       raw,
		MimeType:   mimeType,
		Extension:  extension,
		DurationMs: durationMs,
	}
}

func audioExtension(mimeType, sourceFormat string) string {
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
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	}
	switch strings.ToLower(strings.TrimSpace(sourceFormat)) {
	case "mp3", "mpeg":
		return ".mp3"
	case "ogg":
		return ".ogg"
	case "opus":
		return ".opus"
	case "webm":
		return ".webm"
	case "pcm", "pcm16", "l16":
		return ".pcm"
	default:
		return ".wav"
	}
}

func audioMimeType(extension string) string {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".webm":
		return "audio/webm"
	case ".pcm":
		return "audio/L16"
	default:
		return "audio/wav"
	}
}
