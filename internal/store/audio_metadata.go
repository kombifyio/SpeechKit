package store

import "os"

func buildLocalAudioAsset(path string, durationMs int64) *AudioAsset {
	if path == "" {
		return nil
	}

	asset := &AudioAsset{
		StorageKind: AudioStorageLocalFile,
		Path:        path,
		MimeType:    "audio/wav",
		DurationMs:  durationMs,
	}
	if info, err := os.Stat(path); err == nil {
		asset.SizeBytes = info.Size()
	}
	return asset
}
