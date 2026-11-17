package format

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- synthetic container builders -------------------------------------
//
// Fixtures are hand-built minimal containers: the repo convention is
// everything generated on the fly, no binary blobs. Whether ffprobe can
// read them is checked separately in oracle_test.go.

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// mp4Box wraps payload in an ISO box: big-endian 32-bit size + type.
func mp4Box(typ string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
	copy(b[4:8], typ)
	return append(b, payload...)
}

// mp4FullBox wraps payload as a version-0 FullBox (version+flags added).
func mp4FullBox(typ string, payload []byte) []byte {
	return mp4Box(typ, append([]byte{0, 0, 0, 0}, payload...))
}

// mvhdPayload builds a movie header: version 0 or 1, identity matrix,
// with timescale and duration ticks.
func mvhdPayload(version byte, timescale uint32, duration uint64) []byte {
	if version == 0 {
		p := []byte{0, 0, 0, 0}
		p = appendU32(p, 0) // creation
		p = appendU32(p, 0) // modification
		p = appendU32(p, timescale)
		p = appendU32(p, uint32(duration))
		p = appendU32(p, 0x00010000)       // rate 1.0
		p = append(p, 0x01, 0x00)          // volume
		p = append(p, make([]byte, 10)...) // reserved
		p = appendU32(p, 0x00010000)       // matrix a
		p = appendU32(p, 0)                // matrix b
		p = appendU32(p, 0)                // matrix u
		p = appendU32(p, 0)                // matrix c
		p = appendU32(p, 0x00010000)       // matrix d
		p = appendU32(p, 0)                // matrix v
		p = appendU32(p, 0)                // matrix e
		p = appendU32(p, 0)                // matrix f
		p = appendU32(p, 0x40000000)       // matrix g
		p = append(p, make([]byte, 6)...)  // predefined
		p = appendU32(p, 0xFFFFFFFF)       // next track id
		return p
	}
	p := []byte{1, 0, 0, 0}
	p = appendU64(p, 0) // creation
	p = appendU64(p, 0) // modification
	p = appendU32(p, timescale)
	p = appendU64(p, duration)
	p = appendU32(p, 0x00010000) // rate
	p = append(p, 0x01, 0x00)    // volume
	p = append(p, make([]byte, 10)...)
	p = appendU32(p, 0x00010000)
	p = appendU32(p, 0)
	p = appendU32(p, 0)
	p = appendU32(p, 0)
	p = appendU32(p, 0x00010000)
	p = appendU32(p, 0)
	p = appendU32(p, 0)
	p = appendU32(p, 0)
	p = appendU32(p, 0x40000000)
	p = append(p, make([]byte, 6)...)
	p = appendU32(p, 0xFFFFFFFF)
	return p
}

func appendU64(b []byte, v uint64) []byte {
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], v)
	return append(b, t[:]...)
}

// buildMP4 assembles a file: ftyp + moov(mvhd + optional title) + mdat.
// quicktimeTitle uses the udta layout, otherwise the iTunes meta/ilst one;
// moovAtEnd places moov after mdat, the recording-typical layout.
func buildMP4(timescale uint32, ticks uint64, title string, opts ...bool) []byte {
	quicktimeTitle, moovAtEnd := true, false
	for i, o := range opts {
		switch i {
		case 0:
			quicktimeTitle = o
		case 1:
			moovAtEnd = o
		}
	}

	moovPayload := mp4Box("mvhd", mvhdPayload(0, timescale, ticks))
	if title != "" {
		if quicktimeTitle {
			moovPayload = append(moovPayload, mp4Box("udta", mp4Box(namType, []byte(title)))...)
		} else {
			data := mp4Box("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(title)...))
			nam := mp4Box(namType, data)
			ilst := mp4Box("ilst", nam)
			// meta is a FullBox whose children are hdlr and ilst.
			meta := mp4FullBox("meta", append(mp4FullBox("hdlr", make([]byte, 25)), ilst...))
			moovPayload = append(moovPayload, meta...)
		}
	}
	moov := mp4Box("moov", moovPayload)
	ftyp := mp4Box("ftyp", []byte("isom\x00\x00\x00\x00"))
	mdat := mp4Box("mdat", []byte("fake media payload"))
	if moovAtEnd {
		return append(append(ftyp, mdat...), moov...)
	}
	return append(append(ftyp, moov...), mdat...)
}

// --- EBML ---------------------------------------------------------------

func appendU32LE(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// ebmlElement encodes id + size + payload. Ids carry their marker bit,
// so the byte length follows from the id's value range.
func ebmlElement(id uint32, payload []byte) []byte {
	var idLen int
	switch {
	case id >= 0x10000000:
		idLen = 4
	case id >= 0x200000:
		idLen = 3
	case id >= 0x4000:
		idLen = 2
	default:
		idLen = 1
	}
	idb := make([]byte, idLen)
	v := uint64(id)
	for i := idLen - 1; i >= 0; i-- {
		idb[i] = byte(v)
		v >>= 8
	}
	size := ebmlSizeBytes(uint64(len(payload)))
	out := append(idb, size...)
	return append(out, payload...)
}

// ebmlSizeBytes encodes a data size vint: marker bit set at the top of
// the chosen width.
func ebmlSizeBytes(v uint64) []byte {
	for width := 1; width <= 8; width++ {
		if v>>uint(7*width) == 0 {
			n := v | uint64(1)<<uint(7*width)
			b := make([]byte, width)
			for i := width - 1; i >= 0; i-- {
				b[i] = byte(n)
				n >>= 8
			}
			return b
		}
	}
	panic("size does not fit in 8 bytes")
}

func ebmlHeader() []byte {
	body := ebmlElement(0x4286, []byte{1})                          // EBMLVersion
	body = append(body, ebmlElement(0x42F7, []byte{1})...)          // EBMLReadVersion
	body = append(body, ebmlElement(0x42F2, []byte{4})...)          // EBMLMaxIDLength
	body = append(body, ebmlElement(0x42F3, []byte{8})...)          // EBMLMaxSizeLength
	body = append(body, ebmlElement(0x4282, []byte("matroska"))...) // DocType
	body = append(body, ebmlElement(0x4287, []byte{2})...)          // DocTypeVersion
	body = append(body, ebmlElement(0x4285, []byte{2})...)          // DocTypeReadVersion
	return ebmlElement(idEBMLHeader, body)
}

// buildMKV assembles header + Segment(Info), or a Segment of unknown
// size (the live-stream shape) when unknownSize is set.
func buildMKV(scale uint32, durationSec float64, title string, unknownSize bool) []byte {
	info := []byte{}
	if scale != 0 {
		info = append(info, ebmlElement(idTimecodeScale, []byte{byte(scale >> 24), byte(scale >> 16), byte(scale >> 8), byte(scale)})...)
	}
	if durationSec > 0 {
		var t [8]byte
		binary.BigEndian.PutUint64(t[:], math.Float64bits(durationSec))
		info = append(info, ebmlElement(idDuration, t[:])...)
	}
	if title != "" {
		info = append(info, ebmlElement(idTitle, []byte(title))...)
	}
	info = append(info, ebmlElement(0x4D80, []byte("diadem-test"))...) // MuxingApp
	info = append(info, ebmlElement(0x5741, []byte("diadem-test"))...) // WritingApp

	if unknownSize {
		// Segment with the all-ones size vint (0xFF): runs to EOF.
		seg := append([]byte{0x18, 0x53, 0x80, 0x67, 0xFF}, ebmlElement(idInfo, info)...)
		return append(ebmlHeader(), seg...)
	}
	return append(ebmlHeader(), ebmlElement(idSegment, ebmlElement(idInfo, info))...)
}

// --- RIFF/AVI ------------------------------------------------------------

func aviChunk(cc string, payload []byte) []byte {
	b := make([]byte, 8+len(payload)+len(payload)&1)
	copy(b[0:4], cc)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

func aviList(listType string, children []byte) []byte {
	return aviChunk("LIST", append([]byte(listType), children...))
}

// buildAVI assembles RIFF(AVI ): hdrl(avih [+ odml/dmlh]) + INFO(INAM) + mdat.
func buildAVI(usecPerFrame, totalFrames uint32, dmlhFrames uint32, title string) []byte {
	avih := make([]byte, 0, 56)
	avih = appendU32LE(avih, usecPerFrame) // dwMicroSecPerFrame
	avih = appendU32LE(avih, 1_000_000)    // dwMaxBytesPerSec
	avih = appendU32LE(avih, 0)            // dwPaddingGranularity
	avih = appendU32LE(avih, 0x10)         // dwFlags: AVIF_HASINDEX
	avih = appendU32LE(avih, totalFrames)  // dwTotalFrames
	avih = appendU32LE(avih, 0)            // dwInitialFrames
	avih = appendU32LE(avih, 1)            // dwStreams
	avih = appendU32LE(avih, 0)            // dwSuggestedBufferSize
	avih = appendU32LE(avih, 640)          // dwWidth
	avih = appendU32LE(avih, 480)          // dwHeight
	for i := 0; i < 4; i++ {
		avih = appendU32LE(avih, 0) // reserved
	}

	hdrl := aviChunk("avih", avih)
	if dmlhFrames != 0 {
		dmlh := aviChunk("dmlh", appendU32LE(nil, dmlhFrames))
		hdrl = append(hdrl, aviList("odml", dmlh)...)
	}

	body := aviList("hdrl", hdrl)
	if title != "" {
		body = append(body, aviList("INFO", aviChunk("INAM", []byte(title)))...)
	}
	body = append(body, aviChunk("JUNK", make([]byte, 16))...)
	body = append(body, aviChunk("movi", []byte("00dcfake"))...) // NB: not a LIST; parsers must skip
	body = append(body, aviChunk("idx1", make([]byte, 16))...)

	riff := make([]byte, 0, 12+len(body))
	riff = append(riff, "RIFF"...)
	riff = appendU32LE(riff, uint32(4+len(body)))
	riff = append(riff, "AVI "...)
	return append(riff, body...)
}

// --- helpers --------------------------------------------------------------

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- MP4 -------------------------------------------------------------------

func TestReadMP4(t *testing.T) {
	path := writeFile(t, "clip.mp4", buildMP4(1000, 5500, "Тест & Title"))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 5500*time.Millisecond {
		t.Errorf("Duration = %v, want 5.5s", meta.Duration)
	}
	if meta.Title != "Тест & Title" {
		t.Errorf("Title = %q", meta.Title)
	}
}

func TestReadMP4NoTitle(t *testing.T) {
	path := writeFile(t, "clip.mp4", buildMP4(48000, 48000*90, ""))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 90*time.Second || meta.Title != "" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestReadMP4ITunesTitleAndMoovAtEnd(t *testing.T) {
	// mdat between ftyp and moov forces the parser to seek past a real
	// payload; the title lives in the iTunes meta/ilst layout.
	path := writeFile(t, "clip.mp4", buildMP4(1000, 12000, "iTunes Title", false, true))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 12*time.Second {
		t.Errorf("Duration = %v", meta.Duration)
	}
	if meta.Title != "iTunes Title" {
		t.Errorf("Title = %q", meta.Title)
	}
}

func TestReadMP4UnknownDuration(t *testing.T) {
	// Fragmented MP4s mark the duration as all-ones.
	path := writeFile(t, "clip.mp4", buildMP4(1000, 0xFFFFFFFF, ""))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 0 {
		t.Errorf("Duration = %v, want unknown (0)", meta.Duration)
	}
}

func TestReadMP4Version1MVHD(t *testing.T) {
	// Hand-assemble a v1 mvhd: 64-bit ticks.
	box := mp4Box("mvhd", mvhdPayload(1, 1000, 61_000))
	fake := append(mp4Box("ftyp", []byte("isom\x00\x00\x00\x00")), mp4Box("moov", box)...)
	path := writeFile(t, "clip.mp4", []byte(fake))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 61*time.Second {
		t.Errorf("Duration = %v, want 61s", meta.Duration)
	}
}

func TestReadMP4Garbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("not an mp4 at all, just bytes"),
		{0, 0, 0, 8, 'm', 'd', 'a', 't'}, // mdat only: no moov
		{0, 0, 0, 3, 'm', 'o', 'o', 'v'}, // size smaller than a header
	}
	for i, data := range cases {
		path := writeFile(t, "broken.mp4", data)
		if _, err := Read(path); err == nil {
			t.Errorf("case %d: want error, got nil", i)
		}
	}
}

// --- MKV -------------------------------------------------------------------

func TestReadMKV(t *testing.T) {
	// 89,750.5 ticks of the EBML default 1ms = 89.7505s.
	path := writeFile(t, "clip.mkv", buildMKV(1_000_000, 89_750.5, "Матрёшка", false))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := 89*time.Second + 750*time.Millisecond + 500*time.Microsecond; meta.Duration != want {
		t.Errorf("Duration = %v, want %v", meta.Duration, want)
	}
	if meta.Title != "Матрёшка" {
		t.Errorf("Title = %q", meta.Title)
	}
}

func TestReadMKVDefaultsAndNoTitle(t *testing.T) {
	// No TimecodeScale element: the 1ms default applies.
	path := writeFile(t, "clip.mkv", buildMKV(0, 1500.25, "", false))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 1500*time.Millisecond+250*time.Microsecond {
		t.Errorf("Duration = %v", meta.Duration)
	}
	if meta.Title != "" {
		t.Errorf("Title = %q", meta.Title)
	}
}

func TestReadMKVUnknownSegmentSize(t *testing.T) {
	// 42,500 ticks of 1ms = 42.5s.
	path := writeFile(t, "live.webm", buildMKV(1_000_000, 42_500, "Live", true))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 42*time.Second+500*time.Millisecond || meta.Title != "Live" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestReadMKVGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("plain text masquerading as matroska"),
		{0x1A, 0x45, 0xDF}, // truncated EBML header id
	}
	for i, data := range cases {
		path := writeFile(t, "broken.mkv", data)
		if _, err := Read(path); err == nil {
			t.Errorf("case %d: want error, got nil", i)
		}
	}
}

// --- AVI -------------------------------------------------------------------

func TestReadAVI(t *testing.T) {
	// 300 frames × 40ms = 12s.
	path := writeFile(t, "clip.avi", buildAVI(40_000, 300, 0, "Старый добрый AVI"))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 12*time.Second {
		t.Errorf("Duration = %v, want 12s", meta.Duration)
	}
	if meta.Title != "Старый добрый AVI" {
		t.Errorf("Title = %q", meta.Title)
	}
}

func TestReadAVIOdmlOverridesFrameCount(t *testing.T) {
	// avih says 10 frames, the OpenDML dmlh says 100: trust dmlh.
	path := writeFile(t, "vbr.avi", buildAVI(40_000, 10, 100, ""))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 4*time.Second {
		t.Errorf("Duration = %v, want 4s", meta.Duration)
	}
}

func TestReadAVIZeroFrames(t *testing.T) {
	path := writeFile(t, "odd.avi", buildAVI(40_000, 0, 0, ""))
	meta, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Duration != 0 {
		t.Errorf("Duration = %v, want unknown (0)", meta.Duration)
	}
}

func TestReadAVIGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("RIFFxxxxWAVEfmt "),         // RIFF, but a WAV
		[]byte("RIFF\x04\x00\x00\x00AVI "), // header claims 4 bytes: no hdrl
	}
	for i, data := range cases {
		path := writeFile(t, "broken.avi", data)
		if _, err := Read(path); err == nil {
			t.Errorf("case %d: want error, got nil", i)
		}
	}
}

// --- dispatch and text decoding --------------------------------------------

func TestReadUnsupportedExtension(t *testing.T) {
	path := writeFile(t, "notes.txt", []byte("hello"))
	if _, err := Read(path); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestDecodeText(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("plain"), "plain"},
		{[]byte("pad\x00\x00"), "pad"},
		{[]byte("  spaced  "), "spaced"},
		{[]byte{0xFF, 0xFE, 'h', 0, 'i', 0}, "hi"}, // UTF-16 LE
		{[]byte{0xFE, 0xFF, 0, 'h', 0, 'i'}, "hi"}, // UTF-16 BE
		{[]byte("bad\xff\xfebytes"), "badbytes"},   // invalid UTF-8 dropped
		{[]byte{0xFF, 0xFE, 0, 0}, ""},             // empty UTF-16 → no title
	}
	for _, c := range cases {
		if got := decodeText(c.in); got != c.want {
			t.Errorf("decodeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
