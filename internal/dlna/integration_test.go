package dlna

import (
	"bytes"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandaCaffeine/diadem/internal/httpd"
)

// The media library the integration tests browse.
var testFiles = map[string]int{
	"movie.mkv":             7,
	"music/track.mp3":       3,
	"movies/Игрушка 4к.mkv": 42,
	"notes.txt":             1,
	".hidden/x.mkv":         1,
}

// newStack assembles the same mux main.go builds: /files/ plus the DLNA
// endpoints, on a real listener so BaseURL is exact.
func newStack(t *testing.T) (baseURL string, root string) {
	t.Helper()
	root = t.TempDir()
	for rel, size := range testFiles {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	baseURL = "http://" + listener.Addr().String()

	dlnaServer := New(Config{Root: root, BaseURL: baseURL, FriendlyName: "Diadem", UUID: "uuid:deadbeef-0000-4000-8000-000000000000"})
	mux := http.NewServeMux()
	mux.Handle("/files/", httpd.New(root).Handler())
	mux.Handle("/", dlnaServer)

	ts := httptest.NewUnstartedServer(mux)
	ts.Listener = listener
	ts.Start()
	t.Cleanup(ts.Close)
	return baseURL, root
}

func soapBrowse(t *testing.T, baseURL, objectID, flag string) (*http.Response, string) {
	t.Helper()
	body := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">` +
		`<ObjectID>` + escString(objectID) + `</ObjectID>` +
		`<BrowseFlag>` + flag + `</BrowseFlag>` +
		`<Filter>*</Filter><StartingIndex>0</StartingIndex><RequestedCount>0</RequestedCount><SortCriteria></SortCriteria>` +
		`</u:Browse></s:Body></s:Envelope>`
	req, err := http.NewRequest(http.MethodPost, baseURL+"/ctl/ContentDirectory", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#Browse"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

// decodeDIDL unescapes the Result string and parses it as a document.
func decodeDIDL(t *testing.T, soapBody string) (result string, doc didlDoc) {
	t.Helper()
	var env struct {
		Result        string `xml:"Body>BrowseResponse>Result"`
		NumberReturns int    `xml:"Body>BrowseResponse>NumberReturned"`
		Total         int    `xml:"Body>BrowseResponse>TotalMatches"`
	}
	if err := xml.Unmarshal([]byte(soapBody), &env); err != nil {
		t.Fatalf("cannot parse SOAP response: %v\n%s", err, soapBody)
	}
	result = env.Result
	if err := xml.Unmarshal([]byte(result), &doc); err != nil {
		t.Fatalf("cannot parse DIDL-Lite: %v\n%s", err, result)
	}
	doc.numberReturned, doc.totalMatches = env.NumberReturns, env.Total
	return result, doc
}

type didlDoc struct {
	XMLName                      xml.Name
	Containers                   []containerX `xml:"container"`
	Items                        []itemX      `xml:"item"`
	numberReturned, totalMatches int
}

type containerX struct {
	ID         string `xml:"id,attr"`
	ParentID   string `xml:"parentID,attr"`
	ChildCount int    `xml:"childCount,attr"`
	Title      string `xml:"title"`
	Class      string `xml:"class"`
}

type itemX struct {
	ID       string `xml:"id,attr"`
	ParentID string `xml:"parentID,attr"`
	Title    string `xml:"title"`
	Class    string `xml:"class"`
	Res      struct {
		Size         int64  `xml:"size,attr"`
		ProtocolInfo string `xml:"protocolInfo,attr"`
		URL          string `xml:",chardata"`
	} `xml:"res"`
}

func TestDescriptionXML(t *testing.T) {
	baseURL, _ := newStack(t)
	resp, err := http.Get(baseURL + "/description.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var desc struct {
		URLBase string `xml:"URLBase"`
		Device  struct {
			DeviceType  string `xml:"deviceType"`
			Friendly    string `xml:"friendlyName"`
			UDN         string `xml:"UDN"`
			ServiceList []struct {
				ServiceType string `xml:"serviceType"`
				SCPDURL     string `xml:"SCPDURL"`
				ControlURL  string `xml:"controlURL"`
			} `xml:"serviceList>service"`
		} `xml:"device"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, &desc); err != nil {
		t.Fatalf("description.xml does not parse: %v\n%s", err, body)
	}
	if desc.URLBase != baseURL+"/" {
		t.Errorf("URLBase = %q, want %q", desc.URLBase, baseURL+"/")
	}
	if desc.Device.Friendly != "Diadem" || !strings.HasPrefix(desc.Device.UDN, "uuid:") {
		t.Errorf("device = %q / %q", desc.Device.Friendly, desc.Device.UDN)
	}
	if desc.Device.DeviceType != "urn:schemas-upnp-org:device:MediaServer:1" {
		t.Errorf("deviceType = %q", desc.Device.DeviceType)
	}
	if len(desc.Device.ServiceList) != 2 {
		t.Errorf("services = %d, want 2", len(desc.Device.ServiceList))
	}
}

func TestBrowseRoot(t *testing.T) {
	baseURL, _ := newStack(t)
	resp, body := soapBrowse(t, baseURL, "0", "BrowseDirectChildren")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d\n%s", resp.StatusCode, body)
	}
	_, doc := decodeDIDL(t, body)

	if doc.numberReturned != 3 || doc.totalMatches != 3 {
		t.Errorf("returned=%d total=%d, want 3/3 (music, movies + movie.mkv)", doc.numberReturned, doc.totalMatches)
	}
	if len(doc.Containers) != 2 || len(doc.Items) != 1 {
		t.Fatalf("containers=%v items=%v", doc.Containers, doc.Items)
	}
	if doc.Containers[0].Title != "movies" || doc.Containers[0].ChildCount != 1 {
		t.Errorf("first container = %+v, want movies with 1 child", doc.Containers[0])
	}
	item := doc.Items[0]
	if item.Title != "movie.mkv" || item.ParentID != "0" {
		t.Errorf("item = %+v", item)
	}
	if !strings.HasPrefix(item.Res.URL, baseURL+"/files/") {
		t.Errorf("res URL = %q, want under %s/files/", item.Res.URL, baseURL)
	}
	if !strings.Contains(item.Res.ProtocolInfo, "video/x-matroska") {
		t.Errorf("protocolInfo = %q", item.Res.ProtocolInfo)
	}
}

func TestBrowseCyrillicContainer(t *testing.T) {
	baseURL, _ := newStack(t)
	_, body := soapBrowse(t, baseURL, "movies", "BrowseDirectChildren")
	_, doc := decodeDIDL(t, body)
	if len(doc.Items) != 1 {
		t.Fatalf("items = %+v", doc.Items)
	}
	item := doc.Items[0]
	if item.Title != "Игрушка 4к.mkv" || item.ID != "movies/Игрушка 4к.mkv" {
		t.Errorf("item = %+v", item)
	}

	// The advertised URL must actually serve the file through /files/.
	resp, err := http.Get(item.Res.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || len(data) != 42 {
		t.Errorf("GET res URL: status=%d len=%d, want 200/42", resp.StatusCode, len(data))
	}
}

func TestBrowseMetadata(t *testing.T) {
	baseURL, _ := newStack(t)

	_, body := soapBrowse(t, baseURL, "0", "BrowseMetadata")
	_, doc := decodeDIDL(t, body)
	if len(doc.Containers) != 1 || doc.Containers[0].Title != "Diadem" {
		t.Errorf("root metadata = %+v", doc.Containers)
	}

	_, body = soapBrowse(t, baseURL, "movie.mkv", "BrowseMetadata")
	_, doc = decodeDIDL(t, body)
	if len(doc.Items) != 1 || doc.Items[0].Res.Size != 7 {
		t.Errorf("item metadata = %+v", doc.Items)
	}
}

func TestBrowsePaging(t *testing.T) {
	baseURL, _ := newStack(t)
	// movies holds exactly one item; ask for it piecewise.
	body := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">` +
		`<ObjectID>0</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter>` +
		`<StartingIndex>2</StartingIndex><RequestedCount>1</RequestedCount><SortCriteria></SortCriteria>` +
		`</u:Browse></s:Body></s:Envelope>`
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/ctl/ContentDirectory", strings.NewReader(body))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#Browse"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	_, doc := decodeDIDL(t, string(data))
	if doc.numberReturned != 1 || len(doc.Items) != 1 || doc.Items[0].Title != "movie.mkv" {
		t.Errorf("page = returned %d items %+v", doc.numberReturned, doc.Items)
	}
}

func TestBrowseFaults(t *testing.T) {
	baseURL, _ := newStack(t)

	resp, body := soapBrowse(t, baseURL, "no/such/dir", "BrowseDirectChildren")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("missing object: status = %d, want 500", resp.StatusCode)
	}
	if code := faultCode(t, body); code != 701 {
		t.Errorf("missing object: errorCode = %d, want 701", code)
	}

	_, body = soapBrowse(t, baseURL, "0", "BrowseFlagsAreWrong")
	if code := faultCode(t, body); code != 402 {
		t.Errorf("bad flag: errorCode = %d, want 402", code)
	}

	// Unknown action on a known service.
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/ctl/ContentDirectory",
		strings.NewReader(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:Search xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><ContainerID>0</ContainerID></u:Search></s:Body></s:Envelope>`))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#Search"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	searchBody := readAll(t, resp)
	resp.Body.Close()
	if code := faultCode(t, searchBody); code != 401 {
		t.Errorf("Search: errorCode = %d, want 401", code)
	}
}

func TestConnectionManagerAndMisc(t *testing.T) {
	baseURL, _ := newStack(t)

	post := func(path, action string) (*http.Response, string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path,
			strings.NewReader(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+action+` xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"></u:`+action+`></s:Body></s:Envelope>`))
		req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ConnectionManager:1#`+action+`"`)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp, readAll(t, resp)
	}

	resp, body := post("/ctl/ConnectionManager", "GetProtocolInfo")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "<SourceProtocolInfo>http-get:*:*:*</SourceProtocolInfo>") {
		t.Errorf("GetProtocolInfo: %d %s", resp.StatusCode, body)
	}

	if _, body = post("/ctl/ConnectionManager", "GetCurrentConnectionIDs"); !strings.Contains(body, "<ConnectionIDs>0</ConnectionIDs>") {
		t.Errorf("GetCurrentConnectionIDs: %s", body)
	}

	// SCPDs are served and parse as XML.
	for _, path := range []string{"/scpd/ContentDirectory.xml", "/scpd/ConnectionManager.xml"} {
		scResp, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		scBody, _ := io.ReadAll(scResp.Body)
		scResp.Body.Close()
		if scResp.StatusCode != http.StatusOK || !bytes.Contains(scBody, []byte("<actionList>")) {
			t.Errorf("GET %s: %d", path, scResp.StatusCode)
		}
	}

	// Method and route handling.
	getResp, _ := http.Get(baseURL + "/ctl/ContentDirectory")
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET control URL: %d, want 405", getResp.StatusCode)
	}
	nfResp, _ := http.Get(baseURL + "/nope")
	nfResp.Body.Close()
	if nfResp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /nope: %d, want 404", nfResp.StatusCode)
	}
}

func faultCode(t *testing.T, soapBody string) int {
	t.Helper()
	var env struct {
		Code int `xml:"Body>Fault>detail>UPnPError>errorCode"`
	}
	if err := xml.Unmarshal([]byte(soapBody), &env); err != nil {
		t.Fatalf("cannot parse fault: %v\n%s", err, soapBody)
	}
	return env.Code
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNewUUIDGenerates(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewUUID()
		if !strings.HasPrefix(id, "uuid:") || len(id) != 36+5 {
			t.Fatalf("bad UUID %q", id)
		}
		if id[19] != '4' { // version nibble at uuid:xxxxxxxx-xxxx-4xxx
			t.Fatalf("UUID %q is not version 4", id)
		}
		if seen[id] {
			t.Fatal("duplicate UUID generated")
		}
		seen[id] = true
	}
}
