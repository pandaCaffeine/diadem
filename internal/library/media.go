package library

import (
	"path/filepath"
	"strings"
)

// mediaExts maps lowercase file extensions to MediaKind. Subtitles are
// deliberately absent: PLAN.md lists them as a non-goal for v1.
var mediaExts = map[string]MediaKind{
	// video
	".3gp": MediaVideo, ".asf": MediaVideo, ".avi": MediaVideo,
	".divx": MediaVideo, ".f4v": MediaVideo, ".flv": MediaVideo,
	".m2ts": MediaVideo, ".mkv": MediaVideo, ".mov": MediaVideo,
	".mp4": MediaVideo, ".m4v": MediaVideo, ".mpeg": MediaVideo,
	".mpg": MediaVideo, ".mts": MediaVideo, ".ogv": MediaVideo,
	".rm": MediaVideo, ".rmvb": MediaVideo, ".ts": MediaVideo,
	".vob": MediaVideo, ".webm": MediaVideo, ".wmv": MediaVideo,
	// audio
	".aac": MediaAudio, ".aif": MediaAudio, ".aiff": MediaAudio,
	".alac": MediaAudio, ".ape": MediaAudio, ".flac": MediaAudio,
	".m4a": MediaAudio, ".mp3": MediaAudio, ".oga": MediaAudio,
	".ogg": MediaAudio, ".opus": MediaAudio, ".wav": MediaAudio,
	".wma": MediaAudio,
	// photo
	".avif": MediaPhoto, ".bmp": MediaPhoto, ".gif": MediaPhoto,
	".heic": MediaPhoto, ".heif": MediaPhoto, ".jpeg": MediaPhoto,
	".jpg": MediaPhoto, ".png": MediaPhoto, ".tif": MediaPhoto,
	".tiff": MediaPhoto, ".webp": MediaPhoto,
}

// KindByExt reports the MediaKind for a filename and whether the file is
// media Diadem serves at all. Matching is case-insensitive.
func KindByExt(name string) (MediaKind, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return MediaNone, false
	}
	kind, ok := mediaExts[ext]
	return kind, ok
}
