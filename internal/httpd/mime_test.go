package httpd

import "testing"

func TestContentType(t *testing.T) {
	cases := map[string]string{
		"movie.mkv":     "video/x-matroska",
		"movie.MKV":     "video/x-matroska", // case-insensitive
		"clip.mp4":      "video/mp4",
		"capture.m2ts":  "video/mp2t",
		"track.mp3":     "audio/mpeg",
		"lossless.flac": "audio/flac",
		"photo.jpg":     "image/jpeg",
		"photo.heic":    "image/heic",
		"unknown.xyz":   "application/octet-stream",
		"no-extension":  "application/octet-stream",
	}
	for name, want := range cases {
		if got := contentType(name); got != want {
			t.Errorf("contentType(%q) = %q, want %q", name, got, want)
		}
	}
}
