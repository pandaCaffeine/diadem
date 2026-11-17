package library

import "testing"

func TestEpisodeCode(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		ok       bool
	}{
		{"show.s05e03.name.1080p.bluray.x264.mkv", "S05E03", true},
		{"Show S1E1 Pilot.mkv", "S01E01", true},
		{"show.s05e03e04.double.mkv", "S05E03", true},
		{"show.s05e03-04.double.mkv", "S05E03", true},
		{"ADVENTURE.S10E09.X.mkv", "S10E09", true},
		{"movie.2017.1080p.mkv", "", false},
		{"e03.only.mkv", "", false},
		{"notes.txt", "", false},
		{"xs05e03.mkv", "", false},
		{"anime.s02e1100.mkv", "S02E1100", true},
	}

	for _, tc := range cases {
		title, ok := EpisodeCode(tc.name)
		if ok != tc.ok || title != tc.expected {
			t.Errorf("EpisodeCode(%q) = (%q, %v), want (%q, %v)", tc.name, title, ok, tc.expected, tc.ok)
		}
	}

}
