package dlna

import (
	"net/http"
	"strconv"
)

// ConnectionManager is a mandatory UPnP AV service. VLC mostly ignores it;
// we answer the three standard actions honestly for the clients that ask.
const (
	cmServiceURN = "urn:schemas-upnp-org:service:ConnectionManager:1"

	// sourceProtocolInfo advertises what the server can serve: via plain
	// HTTP, any of the media types the Content Directory offers. v1 keeps
	// it a single wildcard entry.
	sourceProtocolInfo = "http-get:*:*:*"
)

func (s *Server) handleConnectionManager(w http.ResponseWriter, _ *http.Request, req soapRequest) {
	switch req.Action {
	case "GetProtocolInfo":
		writeSOAP(w, http.StatusOK, soapEnvelope(cmServiceURN, "GetProtocolInfo", []soapArg{
			{"SourceProtocolInfo", sourceProtocolInfo},
			{"SinkProtocolInfo", ""},
		}))
	case "GetCurrentConnectionIDs":
		writeSOAP(w, http.StatusOK, soapEnvelope(cmServiceURN, "GetCurrentConnectionIDs", []soapArg{
			{"ConnectionIDs", "0"},
		}))
	case "GetCurrentConnectionInfo":
		writeSOAP(w, http.StatusOK, soapEnvelope(cmServiceURN, "GetCurrentConnectionInfo", []soapArg{
			{"RcsID", "0"},
			{"AVTransportID", "0"},
			{"ProtocolInfo", ""},
			{"PeerConnectionManager", ""},
			{"PeerConnectionID", strconv.Itoa(-1)},
			{"Direction", "Input"},
			{"Status", "OK"},
		}))
	default:
		_, _ = w.Write(soapFault(w, upnpInvalidAction, "Invalid Action"))
	}
}
