package main

import "testing"

func TestVersionString(t *testing.T) {
	// Any build must report something: a stamped version, the module
	// version, a git revision, or plain "dev" — never an empty string.
	if got := versionString(); got == "" {
		t.Error("versionString() is empty")
	}

	// The ldflags-stamped value always wins.
	version = "v9.9.9"
	t.Cleanup(func() { version = "" })
	if got := versionString(); got != "v9.9.9" {
		t.Errorf("versionString() = %q, want v9.9.9", got)
	}
}
