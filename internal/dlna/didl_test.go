package dlna

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pandaCaffeine/diadem/internal/format"
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

	lib, err := library.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s.didlForNodes(lib.Root.Children, "0")

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
		n := &library.Node{Name: "x.bin", Path: "/m/x", Kind: library.Item, Media: kind, Size: 1}
		got := s.itemElement(n, "x", "0")
		if !strings.Contains(got, class) {
			t.Errorf("kind %v: item element misses class %s:\n%s", kind, class, got)
		}
	}
}

func TestItemElementMetadata(t *testing.T) {
	s := New(Config{Root: "/m", BaseURL: "http://h:1", FriendlyName: "Diadem"})

	// Container metadata wins the title, and duration/bitrate appear on
	// the res element. Size 7'000'000 B over 1:30:32 → 1288 B/s.
	n := &library.Node{
		Name: "movie.mkv", Path: "/m/movie.mkv", Kind: library.Item,
		Media: library.MediaVideo, Size: 7_000_000,
		Meta: format.Meta{Title: "Купол", Duration: time.Hour + 30*time.Minute + 32*time.Second},
	}
	got := s.itemElement(n, "movie.mkv", "0")
	for _, want := range []string{
		`<dc:title>Купол</dc:title>`,
		`duration="1:30:32"`,
		`bitrate="1288"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("item element misses %q:\n%s", want, got)
		}
	}

	// Without metadata: the filename stays the title and no extra
	// attributes appear.
	plain := &library.Node{
		Name: "movie.mkv", Path: "/m/movie.mkv", Kind: library.Item,
		Media: library.MediaVideo, Size: 7,
	}
	got = s.itemElement(plain, "movie.mkv", "0")
	if !strings.Contains(got, `<dc:title>movie.mkv</dc:title>`) {
		t.Errorf("filename title lost:\n%s", got)
	}
	if strings.Contains(got, `duration=`) || strings.Contains(got, `bitrate=`) {
		t.Errorf("unknown metadata must not be advertised:\n%s", got)
	}
}

func TestNodeForAndIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "movies", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, size := range map[string]int{
		"a.mkv":            1,
		"movies/b.mkv":     2,
		"movies/sub/c.mkv": 3,
	} {
		if err := os.WriteFile(filepath.Join(dir, rel), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := library.NewStore(dir, nil)
	if _, err := store.Scan(); err != nil {
		t.Fatal(err)
	}
	s := New(Config{Root: dir, BaseURL: "http://h:1", FriendlyName: "Diadem", Source: store})

	if n, ok := s.nodeFor("0"); !ok || n != store.Current().Root {
		t.Errorf("nodeFor(0) = %v %v, want the root", n, ok)
	}
	if n, ok := s.nodeFor("movies"); !ok || n.Name != "movies" || n.Kind != library.Container {
		t.Errorf("nodeFor(movies) = %v %v", n, ok)
	}
	if n, ok := s.nodeFor("movies/sub"); !ok || n.Name != "sub" {
		t.Errorf("nodeFor(movies/sub) = %v %v", n, ok)
	}
	if n, ok := s.nodeFor("movies/b.mkv"); !ok || n.Kind != library.Item || n.Size != 2 {
		t.Errorf("nodeFor(movies/b.mkv) = %v %v", n, ok)
	}
	for _, bad := range []string{"..", "a/../b", "/abs", ".hidden", "a//b", "a\\b", "no/such"} {
		if _, ok := s.nodeFor(bad); ok {
			t.Errorf("nodeFor(%q) accepted", bad)
		}
	}

	// A nil source, or a Store whose first scan has not finished, must
	// miss rather than panic.
	noSource := New(Config{Root: dir, BaseURL: "http://h:1", FriendlyName: "Diadem"})
	if _, ok := noSource.nodeFor("0"); ok {
		t.Error("nil source accepted an id")
	}
	unscanned := New(Config{Root: dir, BaseURL: "http://h:1", FriendlyName: "Diadem", Source: library.NewStore(dir, nil)})
	if _, ok := unscanned.nodeFor("0"); ok {
		t.Error("unscanned store accepted an id")
	}

	// relID and parentIDOf are pure id math; pin them against a fixed root.
	byPath := New(Config{Root: "/media", BaseURL: "http://h:1", FriendlyName: "Diadem"})
	if got := byPath.relID("/media/movies/a.mkv"); got != "movies/a.mkv" {
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
