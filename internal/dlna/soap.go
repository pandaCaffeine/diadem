package dlna

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// UPnP error codes used by Diadem (UPnP AV ConnectionManager /
// ContentDirectory standard codes).
const (
	upnpInvalidAction = 401
	upnpInvalidArgs   = 402
	upnpNoSuchObject  = 701
)

// soapRequest is a parsed UPnP SOAP control request.
type soapRequest struct {
	Action string
	Args   map[string]string
}

// soapArg is one response argument; the order matters to picky clients.
type soapArg struct {
	Name  string
	Value string
}

// actionFromHeader extracts the action name from a SOAPACTION header value
// like "urn:schemas-upnp-org:service:ContentDirectory:1#Browse".
func actionFromHeader(v string) string {
	v = strings.Trim(strings.TrimSpace(v), `"`)
	if i := strings.LastIndexByte(v, '#'); i >= 0 {
		return v[i+1:]
	}
	return ""
}

// parseSOAPBody pulls the action element (the first element inside
// Envelope/Body) and its flat child arguments out of a SOAP request.
// Namespace prefixes are ignored; UPnP arguments never nest.
func parseSOAPBody(body []byte) (action string, args map[string]string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	args = make(map[string]string)
	inBody := false
	var arg string // argument element currently being read, "" when none
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return "", nil, terr
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Local == "Body":
				inBody = true
			case inBody && action == "":
				action = t.Name.Local
			case action != "":
				arg = t.Name.Local
				args[arg] = ""
			}
		case xml.CharData:
			if arg != "" {
				args[arg] += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == arg {
				arg = ""
			}
		}
	}
	if action == "" {
		return "", nil, fmt.Errorf("no action element in SOAP body")
	}
	return action, args, nil
}

// soapEnvelope renders a successful SOAP response for a control action.
func soapEnvelope(service, action string, args []soapArg) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	b.WriteString(`<s:Body><u:` + action + `Response xmlns:u="` + escString(service) + `">`)
	for _, a := range args {
		b.WriteString(`<` + a.Name + `>`)
		esc(&b, a.Value)
		b.WriteString(`</` + a.Name + `>`)
	}
	b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
	return []byte(b.String())
}

// soapFault renders a UPnP error as a SOAP fault; per the UPnP Device
// Architecture it travels with HTTP 500.
func soapFault(w http.ResponseWriter, code int, description string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	b.WriteString(`<s:Body><s:Fault>`)
	b.WriteString(`<faultcode>s:Client</faultcode>`)
	b.WriteString(`<faultstring>UPnPError</faultstring>`)
	b.WriteString(`<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">`)
	b.WriteString(`<errorCode>`)
	b.WriteString(fmt.Sprint(code))
	b.WriteString(`</errorCode>`)
	b.WriteString(`<errorDescription>`)
	b.WriteString(escString(description))
	b.WriteString(`</errorDescription>`)
	b.WriteString(`</UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	return []byte(b.String())
}

func writeSOAP(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// control dispatches a POSTed SOAP request to a service handler.
func (s *Server) control(w http.ResponseWriter, r *http.Request, dispatch func(http.ResponseWriter, *http.Request, soapRequest)) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		_, _ = w.Write(soapFault(w, upnpInvalidArgs, "cannot read request body"))
		return
	}
	action, args, perr := parseSOAPBody(body)
	if perr != nil {
		_, _ = w.Write(soapFault(w, upnpInvalidArgs, "malformed SOAP body"))
		return
	}
	if headerAction := actionFromHeader(r.Header.Get("SOAPACTION")); headerAction != "" {
		action = headerAction // the header is authoritative when present
	}
	if action == "" {
		_, _ = w.Write(soapFault(w, upnpInvalidAction, "no SOAP action"))
		return
	}
	dispatch(w, r, soapRequest{Action: action, Args: args})
}

// esc writes s to b as escaped XML text.
func esc(b *strings.Builder, s string) {
	_ = xml.EscapeText(b, []byte(s)) // never fails on strings.Builder
}
