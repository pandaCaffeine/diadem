package library

import "testing"

func TestKindByExt(t *testing.T) {
	cases := []struct {
		name string
		kind MediaKind
		ok   bool
	}{
		{"movie.mkv", MediaVideo, true},
		{"movie.MKV", MediaVideo, true}, // case-insensitive
		{"clip.m2ts", MediaVideo, true}, // MPEG-TS, not TypeScript
		{"track01.mp3", MediaAudio, true},
		{"lossless.flac", MediaAudio, true},
		{"photo.heic", MediaPhoto, true},
		{"img.jpeg", MediaPhoto, true},
		{"movie.srt", MediaNone, false}, // subtitles are a non-goal
		{"movie.vtt", MediaNone, false},
		{"readme.txt", MediaNone, false},
		{"Makefile", MediaNone, false}, // no extension
		{"archive.tar.gz", MediaNone, false},
	}
	for _, tc := range cases {
		kind, ok := KindByExt(tc.name)
		if ok != tc.ok || kind != tc.kind {
			t.Errorf("KindByExt(%q) = (%v, %v), want (%v, %v)", tc.name, kind, ok, tc.kind, tc.ok)
		}
	}
}
