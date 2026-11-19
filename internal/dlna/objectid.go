package dlna

import (
	"path/filepath"
	"strings"

	"github.com/pandaCaffeine/diadem/internal/library"
)

// rootObjectID identifies the Library root container. Every other
// ObjectID is the object's path relative to the root ("movies",
// "movies/a.mkv"). Reserved-name caveat: a top-level folder literally
// called "0" would collide — accepted for v1.
const rootObjectID = "0"

// browseTarget is the human-readable name of what a Browse touched:
// the Library itself, a Container or an Item.
func (s *Server) browseTarget(id string) string {
	if id == "" || id == rootObjectID {
		return "Library"
	}
	return filepath.Base(id)
}

// nodeFor maps an ObjectID — the root ("0", or the empty id some
// clients send) or a slash-separated chain of Container and Item names —
// to a Node of the current Library snapshot. The tree is the single
// source of truth, so anything unknown or hostile (traversals, empty
// segments) simply misses.
func (s *Server) nodeFor(id string) (*library.Node, bool) {
	if s.cfg.Source == nil {
		return nil, false
	}
	lib := s.cfg.Source.Current()
	if lib == nil {
		return nil, false
	}
	if id == "" || id == rootObjectID {
		return lib.Root, true
	}
	node := lib.Root
	for _, seg := range strings.Split(id, "/") {
		if seg == "" {
			return nil, false
		}
		var next *library.Node
		for _, c := range node.Children {
			if c.Name == seg {
				next = c
				break
			}
		}
		if next == nil {
			return nil, false
		}
		node = next
	}
	return node, true
}

// relID is the ObjectID for an absolute path: its path relative to root.
func (s *Server) relID(path string) string {
	rel, err := filepath.Rel(s.cfg.Root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func objectIDOf(id string) string {
	if id == "" {
		return rootObjectID
	}
	return id
}

func parentIDOf(id string) string {
	if id == "" || id == rootObjectID {
		return "-1" // the root's parent, per convention
	}
	if before, _, ok := strings.CutLast(id, "/"); ok {
		return before
	}
	return rootObjectID
}
