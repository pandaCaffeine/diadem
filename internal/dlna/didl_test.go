package dlna

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandaCaffeine/diadem/internal/library"
)

// Golden DIDL-Lite: exactly what a Browse of the root returns, including
// escaping of cyrillic-and-spaces names.
func TestDIDLForChildrenGolden(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]int{
		"movie.mkv":              7,
		"sub/Игрушка.mp4":        9,
		"sub/Track & Video.flac": 11,
		"notes.txt":              1,
	}
	for rel, size := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := New(Config{
		Root:         dir,
		BaseURL:      "http://192.0.2.1:8200",
		FriendlyName: "Diadem",
	})

	entries, err := library.ReadContainer(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s.didlForChildren(entries, "0")

	want := didlOpen +
		`<container id="sub" parentID="0" restricted="1" childCount="2"><dc:title>sub</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>` +
		`<item id="movie.mkv" parentID="0" restricted="1"><dc:title>movie.mkv</dc:title><upnp:class>object.item.videoItem</upnp:class><res size="7" protocolInfo="http-get:*:video/x-matroska:*">http://192.0.2.1:8200/files/movie.mkv</res></item>` +
		didlClose
	if got != want {
		t.Errorf("DIDL mismatch:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestItemElementKinds(t *testing.T) {
	s := New(Config{Root: "/m", BaseURL: "http://h:1", FriendlyName: "Diadem"})
	cases := map[library.MediaKind]string{
		library.MediaVideo: "object.item.videoItem",
		library.MediaAudio: "object.item.audioItem",
		library.MediaPhoto: "object.item.imageItem",
	}
	for kind, class := range cases {
		e := library.Entry{Name: "x.bin", Path: "/m/x", Kind: library.Item, Media: kind, Size: 1}
		got := s.itemElement(e, "x", "0")
		if !strings.Contains(got, class) {
			t.Errorf("kind %v: item element misses class %s:\n%s", kind, class, got)
		}
	}
}

func TestObjectPathAndIDs(t *testing.T) {
	s := New(Config{Root: "/media", BaseURL: "http://h:1", FriendlyName: "Diadem"})

	if p, ok := s.objectPath("0"); !ok || p != "/media" {
		t.Errorf("objectPath(0) = %q %v, want /media", p, ok)
	}
	if p, ok := s.objectPath("movies/sub"); !ok || p != "/media/movies/sub" {
		t.Errorf("objectPath(movies/sub) = %q %v", p, ok)
	}
	for _, bad := range []string{"..", "a/../b", "/abs", ".hidden", "a/.b", `a\b`, "a//b"} {
		if _, ok := s.objectPath(bad); ok {
			t.Errorf("objectPath(%q) accepted", bad)
		}
	}

	if got := s.relID("/media/movies/a.mkv"); got != "movies/a.mkv" {
		t.Errorf("relID = %q, want movies/a.mkv", got)
	}
	if got := parentIDOf("movies/a.mkv"); got != "movies" {
		t.Errorf("parentIDOf deep = %q, want movies", got)
	}
	if got := parentIDOf("a.mkv"); got != "0" {
		t.Errorf("parentIDOf top = %q, want 0", got)
	}
	if got := parentIDOf("0"); got != "-1" {
		t.Errorf("parentIDOf root = %q, want -1", got)
	}
}
