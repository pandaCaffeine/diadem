package dlna

import (
	"fmt"
	"strings"
	"time"

	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
	"github.com/pandaCaffeine/diadem/internal/watch"
)

const (
	didlOpen  = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`
	didlClose = `</DIDL-Lite>`
)

// didlForNodes renders one page of Container children from the tree.
func (s *Server) didlForNodes(nodes []*library.Node, parentID string) string {
	var inner strings.Builder
	for _, n := range nodes {
		id := s.relID(n.Path)
		if n.Kind == library.Container {
			inner.WriteString(s.containerElement(id, parentID, n.Name, len(n.Children)))
		} else {
			inner.WriteString(s.itemElement(n, id, parentID))
		}
	}
	return didlDocument(inner.String())
}

func didlDocument(inner string) string {
	return didlOpen + inner + didlClose
}

func (s *Server) containerElement(id, parentID, title string, childCount int) string {
	return fmt.Sprintf(
		`<container id="%s" parentID="%s" restricted="1" childCount="%d"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
		escString(id), escString(parentID), childCount, escString(title))
}

// watchedMarker closes the Display Title of a Watched video Item. It is
// a suffix on purpose: Display Title exists so clients sorting by title
// keep episode order, and a prefix on part of the episodes would break
// it. One constant, tuned once after a real TV check (CONTEXT.md:
// Watched).
const watchedMarker = " · ✓"

// displayTitle is what a Client sees as the Item's name: the container's
// own title when muxed in, else the file name; an episode code found in
// the file name always leads, so clients sorting by title keep episode
// order.
func displayTitle(n *library.Node) string {
	result := n.Name
	if n.Meta.Title != "" {
		result = n.Meta.Title
	}

	if code, ok := library.EpisodeCode(n.Name); ok {
		result = code + " · " + result
	}
	return result
}

func (s *Server) itemElement(n *library.Node, id, parentID string) string {
	class := "object.item.videoItem"
	switch n.Media {
	case library.MediaAudio:
		class = "object.item.audioItem"
	case library.MediaPhoto:
		class = "object.item.imageItem"
	}

	title := displayTitle(n)
	if n.Media == library.MediaVideo && s.cfg.IsWatched != nil && s.cfg.IsWatched(watch.KeyFor(id, n)) {
		title += watchedMarker
	}

	protocolInfo := "http-get:*:" + httpd.ContentType(n.Name) + ":*"
	url := s.cfg.BaseURL + httpd.FilePathToURL(s.cfg.Root, n.Path)
	resAttrs := fmt.Sprintf(`size="%d" protocolInfo="%s"`, n.Size, escString(protocolInfo))
	if n.Meta.Duration > 0 {
		resAttrs += fmt.Sprintf(` duration="%s"`, didlDuration(n.Meta.Duration))
		if bps := avgBytesPerSecond(n.Size, n.Meta.Duration); bps > 0 {
			resAttrs += fmt.Sprintf(` bitrate="%d"`, bps)
		}
	}
	return fmt.Sprintf(
		`<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>%s</upnp:class><res %s>%s</res></item>`,
		escString(id), escString(parentID), escString(title), class, resAttrs, escString(url))
}

// didlDuration renders res@duration in the UPnP form H*:MM:SS: unpadded
// hours, two-digit minutes and seconds, floored to whole seconds.
func didlDuration(d time.Duration) string {
	sec := int64(d / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, (sec%3600)/60, sec%60)
}

// avgBytesPerSecond is the average rate: file size over duration. The
// UPnP spec counts res@bitrate in bytes/second (not bits, despite the
// name); the containers do not store a trustworthy rate themselves.
func avgBytesPerSecond(size int64, d time.Duration) int64 {
	sec := int64(d / time.Second)
	if sec < 1 || size <= 0 {
		return 0
	}
	return size / sec
}
