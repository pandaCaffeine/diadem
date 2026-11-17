package library

import (
	"fmt"
	"regexp"
	"strconv"
)

var episodeRe = regexp.MustCompile(`(?i)\bs(\d{1,4})e(\d{1,4})`)

// EpisodeCode extracts the "s##e##" marker from a release-style
// filename and returns it normalized, e.g. "S05E03". The padding keeps
// titles lexically sortable: release packs pad episode numbers to the
// same width within a series.
func EpisodeCode(name string) (string, bool) {
	matches := episodeRe.FindStringSubmatch(name)
	if matches == nil {
		return "", false
	}

	season, _ := strconv.Atoi(matches[1])
	episode, _ := strconv.Atoi(matches[2])

	return fmt.Sprintf("S%02dE%02d", season, episode), true
}
