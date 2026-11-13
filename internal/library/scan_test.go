package library

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "S02E9.mkv"), 100)
	write(t, filepath.Join(dir, "S02E10.mkv"), 200)
	write(t, filepath.Join(dir, "movie.mkv"), 10)
	write(t, filepath.Join(dir, "Игрушка.mp4"), 5)  // cyrillic names are first-class
	write(t, filepath.Join(dir, "readme.txt"), 999) // not media
	write(t, filepath.Join(dir, ".DS_Store"), 999)  // hidden

	for _, sub := range []string{"sub", "music", "empty", "onlyjunk", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "sub", "nature.png"), 50)
	write(t, filepath.Join(dir, "music", "track01.mp3"), 1)
	write(t, filepath.Join(dir, "music", "track2.flac"), 2)
	write(t, filepath.Join(dir, "onlyjunk", "notes.txt"), 1) // pruned with its folder
	write(t, filepath.Join(dir, ".hidden", "secret.mkv"), 1) // skipped whole folder

	lib, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if lib.Items != 7 {
		t.Errorf("Items = %d, want 7", lib.Items)
	}
	if lib.Containers != 2 { // sub, music; empty/onlyjunk/.hidden pruned
		t.Errorf("Containers = %d, want 2", lib.Containers)
	}
	if want := int64(368); lib.Bytes != want {
		t.Errorf("Bytes = %d, want %d", lib.Bytes, want)
	}

	// Root children: containers first (music, sub), then items in natural
	// order (movie.mkv, S02E9.mkv, S02E10.mkv, Игрушка.mp4).
	wantOrder := []string{"music", "sub", "movie.mkv", "S02E9.mkv", "S02E10.mkv", "Игрушка.mp4"}
	if len(lib.Root.Children) != len(wantOrder) {
		t.Fatalf("root has %d children, want %d", len(lib.Root.Children), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got := lib.Root.Children[i].Name; got != want {
			t.Errorf("root child %d = %q, want %q", i, got, want)
		}
	}
}

func TestScanEmptyLibrary(t *testing.T) {
	dir := t.TempDir()
	lib, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Items != 0 || len(lib.Root.Children) != 0 {
		t.Errorf("empty folder produced Items=%d children=%d, want zeros", lib.Items, len(lib.Root.Children))
	}
}

func TestScanSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "real.mkv"), 1)
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "d", "inside.mp3"), 1)

	if err := os.Symlink(filepath.Join(dir, "real.mkv"), filepath.Join(dir, "link.mkv")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "d"), filepath.Join(dir, "linkdir")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}

	lib, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Items != 2 { // real.mkv + d/inside.mp3; link.mkv and linkdir/* skipped
		t.Errorf("Items = %d, want 2", lib.Items)
	}
}

func TestScanErrors(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing folder: want error, got nil")
	}
	file := filepath.Join(t.TempDir(), "notafolder.txt")
	write(t, file, 1)
	if _, err := Scan(file); err == nil {
		t.Error("file instead of folder: want error, got nil")
	}
}
