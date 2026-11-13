package httpd

import (
	"path/filepath"
	"strings"
)

// mimeTypes maps media extensions to MIME types. It is explicit on
// purpose: responses must be identical across operating systems, and the
// system mime database is not. Values follow common DLNA practice.
var mimeTypes = map[string]string{
	// video
	".3gp": "video/3gpp", ".asf": "video/x-ms-asf", ".avi": "video/x-msvideo",
	".divx": "video/x-divx", ".f4v": "video/x-f4v", ".flv": "video/x-flv",
	".m2ts": "video/mp2t", ".mkv": "video/x-matroska", ".mov": "video/quicktime",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mpeg": "video/mpeg",
	".mpg": "video/mpeg", ".mts": "video/mp2t", ".ogv": "video/ogg",
	".rm": "application/vnd.rn-realmedia", ".rmvb": "application/vnd.rn-realmedia-vbr",
	".ts": "video/mp2t", ".vob": "video/mpeg", ".webm": "video/webm",
	".wmv": "video/x-ms-wmv",
	// audio
	".aac": "audio/aac", ".aif": "audio/aiff", ".aiff": "audio/aiff",
	".alac": "audio/mp4", ".ape": "audio/x-monkeys-audio", ".flac": "audio/flac",
	".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".oga": "audio/ogg",
	".ogg": "audio/ogg", ".opus": "audio/opus", ".wav": "audio/wav",
	".wma": "audio/x-ms-wma",
	// photo
	".avif": "image/avif", ".bmp": "image/bmp", ".gif": "image/gif",
	".heic": "image/heic", ".heif": "image/heif", ".jpeg": "image/jpeg",
	".jpg": "image/jpeg", ".png": "image/png", ".tif": "image/tiff",
	".tiff": "image/tiff", ".webp": "image/webp",
}

// contentType returns the MIME type for a filename, falling back to a
// binary stream for anything unknown.
func contentType(name string) string {
	if t, ok := mimeTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}
