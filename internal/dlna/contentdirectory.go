package dlna

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pandaCaffeine/diadem/internal/library"
)

const (
	cdServiceURN = "urn:schemas-upnp-org:service:ContentDirectory:1"

	// systemUpdateID never changes: without GENA eventing there is nobody
	// to notify, and clients treat the value as opaque.
	systemUpdateID = 1
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

// parseIndex reads StartingIndex/RequestedCount: empty means 0; a
// negative or non-numeric value is client garbage and faults with 402
// before it can reach a slice bound.
func parseIndex(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err == nil && n < 0 {
		return 0, fmt.Errorf("negative index %d", n)
	}
	return n, err
}
