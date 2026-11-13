package dlna

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
// children as a DIDL-Lite document.
func (s *Server) browseChildren(w http.ResponseWriter, ip, id string, start, count int) {
	dir, ok := s.objectPath(id)
	if !ok {
		s.faultNoSuchObject(w, ip, id)
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		s.faultNoSuchObject(w, ip, id)
		return
	}
	entries, err := library.ReadContainer(dir)
	if err != nil {
		s.faultNoSuchObject(w, ip, id)
		return
	}

	total := len(entries)
	if start > total {
		start = total
	}
	end := total
	if count > 0 && start+count < end {
		end = start + count
	}
	page := entries[start:end]

	s.cfg.Logger.Logf("[soap] %s browse %q → %d/%d entries", ip, s.browseTarget(id), len(page), total)
	result := s.didlForChildren(page, objectIDOf(id))
	s.writeBrowseResponse(w, result, len(page), total)
}

// browseMetadata answers BrowseMetadata for the root, a Container or a
// single Item.
func (s *Server) browseMetadata(w http.ResponseWriter, ip, id string) {
	abs, ok := s.objectPath(id)
	if !ok {
		s.faultNoSuchObject(w, ip, id)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		s.faultNoSuchObject(w, ip, id)
		return
	}

	var inner string
	switch {
	case info.IsDir():
		title := s.cfg.FriendlyName
		if oid := objectIDOf(id); oid != rootObjectID {
			title = filepath.Base(abs)
		}
		childCount := 0
		if children, cerr := library.ReadContainer(abs); cerr == nil {
			childCount = len(children)
		}
		inner = s.containerElement(objectIDOf(id), parentIDOf(id), title, childCount)
	default:
		kind, isMedia := library.KindByExt(info.Name())
		if !isMedia {
			s.faultNoSuchObject(w, ip, id)
			return
		}
		entry := library.Entry{
			Name: info.Name(), Path: abs,
			Kind: library.Item, Media: kind, Size: info.Size(),
		}
		inner = s.itemElement(entry, objectIDOf(id), parentIDOf(id))
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

// didlForChildren renders one page of Container children.
func (s *Server) didlForChildren(entries []library.Entry, parentID string) string {
	var inner strings.Builder
	for _, e := range entries {
		id := s.relID(e.Path)
		if e.Kind == library.Container {
			inner.WriteString(s.containerElement(id, parentID, e.Name, s.childCount(e.Path)))
		} else {
			inner.WriteString(s.itemElement(e, id, parentID))
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

func (s *Server) itemElement(e library.Entry, id, parentID string) string {
	class := "object.item.videoItem"
	switch e.Media {
	case library.MediaAudio:
		class = "object.item.audioItem"
	case library.MediaPhoto:
		class = "object.item.imageItem"
	}
	protocolInfo := "http-get:*:" + httpd.ContentType(e.Name) + ":*"
	url := s.cfg.BaseURL + httpd.FilePathToURL(s.cfg.Root, e.Path)
	return fmt.Sprintf(
		`<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>%s</upnp:class><res size="%d" protocolInfo="%s">%s</res></item>`,
		escString(id), escString(parentID), escString(e.Name), class, e.Size, escString(protocolInfo), escString(url))
}

// childCount is the number of direct children of a Container. It re-lists
// the folder; fine for home libraries, cacheable later if needed.
func (s *Server) childCount(dir string) int {
	children, err := library.ReadContainer(dir)
	if err != nil {
		return 0
	}
	return len(children)
}

// browseTarget is the human-readable name of what a Browse touched:
// the Library itself, a Container or an Item.
func (s *Server) browseTarget(id string) string {
	if id == "" || id == rootObjectID {
		return "Library"
	}
	return filepath.Base(id)
}

// objectPath maps an ObjectID to an absolute path inside the Library.
// The root (and the empty id some clients send) is the Library itself.
func (s *Server) objectPath(id string) (string, bool) {
	if id == "" || id == rootObjectID {
		return s.cfg.Root, true
	}
	segs := strings.Split(id, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", false
		}
		if strings.ContainsAny(seg, "\\\x00") {
			return "", false
		}
	}
	abs := filepath.Join(s.cfg.Root, filepath.Join(segs...))
	if !strings.HasPrefix(abs, s.cfg.Root+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
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
