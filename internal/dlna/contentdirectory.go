package dlna

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pandaCaffeine/diadem/internal/httpd"
	"github.com/pandaCaffeine/diadem/internal/library"
)

const (
	cdServiceURN = "urn:schemas-upnp-org:service:ContentDirectory:1"

	// rootObjectID identifies the Library root container. Every other
	// ObjectID is the object's path relative to the root ("movies",
	// "movies/a.mkv"). Reserved-name caveat: a top-level folder literally
	// called "0" would collide — accepted for v1.
	rootObjectID = "0"

	// systemUpdateID never changes: without GENA eventing there is nobody
	// to notify, and clients treat the value as opaque.
	systemUpdateID = 1
)

const (
	didlOpen  = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`
	didlClose = `</DIDL-Lite>`
)

func (s *Server) handleContentDirectory(w http.ResponseWriter, r *http.Request, req soapRequest) {
	switch req.Action {
	case "Browse":
		s.actionBrowse(w, r, req)
	default:
		s.cfg.Logger.Logf("[soap] %s action %q → 401", clientIP(r.RemoteAddr), req.Action)
		_, _ = w.Write(soapFault(w, upnpInvalidAction, "Invalid Action"))
	}
}

func (s *Server) actionBrowse(w http.ResponseWriter, r *http.Request, req soapRequest) {
	ip := clientIP(r.RemoteAddr)
	id := strings.TrimSpace(req.Args["ObjectID"])
	flag := strings.TrimSpace(req.Args["BrowseFlag"])
	start, errStart := parseIndex(req.Args["StartingIndex"])
	count, errCount := parseIndex(req.Args["RequestedCount"])
	if errStart != nil || errCount != nil {
		s.cfg.Logger.Logf("[soap] %s Browse %q bad indexes → 402", ip, id)
		_, _ = w.Write(soapFault(w, upnpInvalidArgs, "Invalid Args"))
		return
	}

	if s.cfg.Observer != nil {
		s.cfg.Observer.ObserveBrowse(ip, r.UserAgent(), s.browseTarget(id))
	}

	switch flag {
	case "BrowseDirectChildren":
		s.browseChildren(w, ip, id, start, count)
	case "BrowseMetadata":
		s.browseMetadata(w, ip, id)
	default:
		s.cfg.Logger.Logf("[soap] %s Browse %q flag %q → 402", ip, id, flag)
		_, _ = w.Write(soapFault(w, upnpInvalidArgs, "Invalid Args"))
	}
}

// browseChildren answers BrowseDirectChildren: one page of a Container's
// children as a DIDL-Lite document, read from the Library snapshot.
func (s *Server) browseChildren(w http.ResponseWriter, ip, id string, start, count int) {
	node, ok := s.nodeFor(id)
	if !ok || node.Kind != library.Container {
		s.faultNoSuchObject(w, ip, id)
		return
	}

	children := node.Children
	total := len(children)
	if start > total {
		start = total
	}
	end := total
	if count > 0 && start+count < end {
		end = start + count
	}
	page := children[start:end]

	s.cfg.Logger.Logf("[soap] %s browse %q → %d/%d entries", ip, s.browseTarget(id), len(page), total)
	result := s.didlForNodes(page, objectIDOf(id))
	s.writeBrowseResponse(w, result, len(page), total)
}

// browseMetadata answers BrowseMetadata for the root, a Container or a
// single Item.
func (s *Server) browseMetadata(w http.ResponseWriter, ip, id string) {
	node, ok := s.nodeFor(id)
	if !ok {
		s.faultNoSuchObject(w, ip, id)
		return
	}

	var inner string
	if node.Kind == library.Container {
		title := s.cfg.FriendlyName
		if oid := objectIDOf(id); oid != rootObjectID {
			title = node.Name
		}
		inner = s.containerElement(objectIDOf(id), parentIDOf(id), title, len(node.Children))
	} else {
		inner = s.itemElement(node, objectIDOf(id), parentIDOf(id))
	}

	s.cfg.Logger.Logf("[soap] %s metadata %q", ip, s.browseTarget(id))
	s.writeBrowseResponse(w, didlDocument(inner), 1, 1)
}

// faultNoSuchObject logs and answers the common 701.
func (s *Server) faultNoSuchObject(w http.ResponseWriter, ip, id string) {
	s.cfg.Logger.Logf("[soap] %s browse %q → 701 no such object", ip, id)
	_, _ = w.Write(soapFault(w, upnpNoSuchObject, "No Such Object"))
}

func (s *Server) writeBrowseResponse(w http.ResponseWriter, result string, returned, total int) {
	writeSOAP(w, http.StatusOK, soapEnvelope(cdServiceURN, "Browse", []soapArg{
		{"Result", result},
		{"NumberReturned", strconv.Itoa(returned)},
		{"TotalMatches", strconv.Itoa(total)},
		{"UpdateID", strconv.Itoa(systemUpdateID)},
	}))
}

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
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[:i]
	}
	return rootObjectID
}

func parseIndex(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// escString returns s escaped as XML text or attribute content.
func escString(s string) string {
	var b strings.Builder
	esc(&b, s)
	return b.String()
}
