package dlna

import (
	"strings"
	"testing"
)

func TestActionFromHeader(t *testing.T) {
	cases := map[string]string{
		`"urn:schemas-upnp-org:service:ContentDirectory:1#Browse"`: "Browse",
		"urn:schemas-upnp-org:service:ContentDirectory:1#Browse":   "Browse",
		`"urn:...#GetCurrentConnectionIDs"`:                        "GetCurrentConnectionIDs",
		"":                                                         "",
		"no-hash":                                                  "",
	}
	for header, want := range cases {
		if got := actionFromHeader(header); got != want {
			t.Errorf("actionFromHeader(%q) = %q, want %q", header, got, want)
		}
	}
}

const vlcBrowseBody = `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Header/>
  <s:Body>
    <u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
      <ObjectID>кино/серия 1</ObjectID>
      <BrowseFlag>BrowseDirectChildren</BrowseFlag>
      <Filter>*</Filter>
      <StartingIndex>0</StartingIndex>
      <RequestedCount>0</RequestedCount>
      <SortCriteria></SortCriteria>
    </u:Browse>
  </s:Body>
</s:Envelope>`

func TestParseSOAPBody(t *testing.T) {
	action, args, err := parseSOAPBody([]byte(vlcBrowseBody))
	if err != nil {
		t.Fatal(err)
	}
	if action != "Browse" {
		t.Errorf("action = %q, want Browse", action)
	}
	wantArgs := map[string]string{
		"ObjectID":       "кино/серия 1",
		"BrowseFlag":     "BrowseDirectChildren",
		"Filter":         "*",
		"StartingIndex":  "0",
		"RequestedCount": "0",
		"SortCriteria":   "",
	}
	for k, want := range wantArgs {
		if args[k] != want {
			t.Errorf("args[%q] = %q, want %q", k, args[k], want)
		}
	}
}

func TestParseSOAPBodyMalformed(t *testing.T) {
	if _, _, err := parseSOAPBody([]byte("this is not xml")); err == nil {
		t.Error("malformed body should error")
	}
	if _, _, err := parseSOAPBody([]byte(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body></s:Body></s:Envelope>`)); err == nil {
		t.Error("body without an action should error")
	}
}

func TestSoapEnvelopeEscapes(t *testing.T) {
	body := string(soapEnvelope("urn:x:1", "Ping", []soapArg{{"Value", `a<b & "c"`}}))
	if !strings.Contains(body, "&lt;b &amp; &#34;c&#34;") {
		t.Errorf("envelope did not escape XML specials:\n%s", body)
	}
	if !strings.Contains(body, `<u:PingResponse xmlns:u="urn:x:1">`) {
		t.Errorf("envelope misses the response element:\n%s", body)
	}
}
