// Package library builds the Diadem Library: a tree of Containers
// (folders) and Items (media files) mirroring a folder on disk.
//
// Terminology follows CONTEXT.md: a Container mirrors a folder, an Item
// mirrors a single media file (video, audio or photo).
package library

import (
	"time"

	"github.com/pandaCaffeine/diadem/internal/format"
)

// MediaKind classifies an Item. See CONTEXT.md.
type MediaKind uint8

const (
	MediaNone MediaKind = iota
	MediaVideo
	MediaAudio
	MediaPhoto
)

func (k MediaKind) String() string {
	switch k {
	case MediaVideo:
		return "video"
	case MediaAudio:
		return "audio"
	case MediaPhoto:
		return "photo"
	}
	return ""
}

// NodeKind distinguishes Containers from Items.
type NodeKind uint8

const (
	Container NodeKind = iota
	Item
)

// Node is one entry of the tree a Client browses.
type Node struct {
	Name     string // display name (base name on disk)
	Path     string // absolute path on disk
	Kind     NodeKind
	Media    MediaKind   // set for Items only
	Size     int64       // set for Items only
	ModTime  time.Time   // set for Items only: replacement detection (Watch State key)
	Meta     format.Meta // video Items only: container title and duration
	Children []*Node     // set for Containers only: Containers first, then Items, natural sort
}

// Library is a scan result: the tree plus aggregate stats.
type Library struct {
	Root  *Node
	Items int
	// Containers counts folders below the root; the root itself is the
	// Library, not one of its folders.
	Containers int
	Bytes      int64
}
