package library

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one browsable child of a folder on disk: a Container that
// transitively holds media, or a media Item. It is the disk-backed cousin
// of Node, used by consumers that browse lazily (the DLNA ContentDirectory).
type Entry struct {
	Name  string
	Path  string // absolute
	Kind  NodeKind
	Media MediaKind // Items only
	Size  int64     // Items only
}

// ReadContainer lists dir with the same visibility rules as Scan: hidden
// entries skipped, only media files, subfolders kept only when they
// contain media somewhere below. Sorted like the TUI tree: Containers
// first, then natural sort by name.
func ReadContainer(dir string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		switch {
		case de.IsDir():
			if HasMedia(path) {
				out = append(out, Entry{Name: name, Path: path, Kind: Container})
			}
		case de.Type().IsRegular():
			kind, ok := KindByExt(name)
			if !ok {
				continue
			}
			size := int64(0)
			if info, ierr := de.Info(); ierr == nil {
				size = info.Size()
			}
			out = append(out, Entry{Name: name, Path: path, Kind: Item, Media: kind, Size: size})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == Container
		}
		return NaturalLess(out[i].Name, out[j].Name)
	})
	return out, nil
}

// HasMedia reports whether dir's subtree holds at least one media Item
// (visibility rules as Scan). It returns as soon as it finds one.
func HasMedia(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	any := false
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if de.IsDir() {
			if HasMedia(filepath.Join(dir, name)) {
				return true
			}
			continue
		}
		if !de.Type().IsRegular() {
			continue
		}
		if _, ok := KindByExt(name); ok {
			any = true
			break
		}
	}
	return any
}
