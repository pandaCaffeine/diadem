package format

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// boxLimit bounds how many sibling boxes are walked per level; a corrupt
// file must fail fast, not spin.
const boxLimit = 4096

// namType is the ISO box id of the title tag, ©nam, in both the
// QuickTime (udta) and iTunes (meta/ilst) layouts.
const namType = "\xa9nam"

// boxLoc describes one ISO base media box found in a file.
type boxLoc struct {
	typ  string
	off  int64 // payload start
	size int64 // payload size
	next int64 // offset of the next sibling box
}

// readBox reads the box header at off. A total size of 1 means a 64-bit
// largesize follows the type; 0 means the box runs to end (end).
func readBox(f io.ReaderAt, off, end int64) (b boxLoc, ok bool) {
	var hdr [16]byte
	if off < 0 || off+8 > end {
		return b, false
	}
	if _, err := f.ReadAt(hdr[:8], off); err != nil {
		return b, false
	}
	total := int64(binary.BigEndian.Uint32(hdr[0:4]))
	b.typ = string(hdr[4:8])
	header := int64(8)
	switch total {
	case 1:
		if _, err := f.ReadAt(hdr[:8], off+8); err != nil {
			return b, false
		}
		total = int64(binary.BigEndian.Uint64(hdr[0:8]))
		header = 16
	case 0:
		total = end - off // extends to the end of the enclosing space
	}
	if total < header || off+total > end {
		return b, false
	}
	b.off = off + header
	b.size = total - header
	b.next = off + total
	return b, true
}

// findBox walks the sibling boxes between begin and end and returns the
// first one named typ.
func findBox(f io.ReaderAt, begin, end int64, typ string) (boxLoc, bool) {
	off := begin
	for i := 0; off < end && i < boxLimit; i++ {
		b, ok := readBox(f, off, end)
		if !ok {
			return boxLoc{}, false
		}
		if b.typ == typ {
			return b, true
		}
		off = b.next
	}
	return boxLoc{}, false
}

// readMP4 parses an ISO base media file (.mp4, .m4v, .mov). It walks box
// headers — seeking over mdat and friends without reading them — to find
// moov, then reads mvhd for the duration and ©nam for the title. moov may
// sit anywhere in the file (end-of-file moov is common for recordings
// that were never faststart-ed), so everything happens through seeks.
func readMP4(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return Meta{}, err
	}
	if end < 8 {
		return Meta{}, errors.New("file too small for an MP4")
	}

	moov, ok := findBox(f, 0, end, "moov")
	if !ok {
		return Meta{}, errors.New("no moov box found: not an MP4")
	}
	moovEnd := moov.off + moov.size

	var meta Meta
	if mvhd, ok := findBox(f, moov.off, moovEnd, "mvhd"); ok {
		payload, err := readAt(f, mvhd.off, min64(mvhd.size, 64))
		if err == nil {
			if d, ok := mvhdDuration(payload); ok {
				meta.Duration = d
			}
		}
	}
	meta.Title = mp4Title(f, moov, moovEnd)
	return meta, nil
}

// mvhdDuration decodes the movie header: timescale (ticks per second)
// and duration in ticks. Version 0 boxes carry 32-bit fields, version 1
// 64-bit ones; an all-ones duration is the "unknown" marker.
func mvhdDuration(p []byte) (time.Duration, bool) {
	if len(p) < 4 {
		return 0, false
	}
	var timescale uint32
	var ticks uint64
	switch p[0] {
	case 0:
		if len(p) < 20 {
			return 0, false
		}
		timescale = binary.BigEndian.Uint32(p[12:16])
		ticks = uint64(binary.BigEndian.Uint32(p[16:20]))
		if ticks == 0xFFFFFFFF {
			return 0, false
		}
	case 1:
		if len(p) < 32 {
			return 0, false
		}
		timescale = binary.BigEndian.Uint32(p[20:24])
		ticks = binary.BigEndian.Uint64(p[24:32])
		if ticks == ^uint64(0) {
			return 0, false
		}
	default:
		return 0, false
	}
	if timescale == 0 || ticks == 0 || ticks > 10*365*24*3600 { // absurd; > 10 years
		return 0, false
	}
	sec := ticks / uint64(timescale)
	ns := uint64(time.Second) * (ticks % uint64(timescale)) / uint64(timescale)
	return time.Duration(sec)*time.Second + time.Duration(ns), true
}

// mp4Title reads the title tag. Muxers store ©nam in one of two layouts:
// QuickTime style as moov/udta/©nam with the text as box payload, or
// iTunes style as an ilst collection — directly under moov/meta, or
// nested one level deeper as moov/udta/meta/ilst (what iTunes-style
// muxers actually write).
func mp4Title(f io.ReaderAt, moov boxLoc, moovEnd int64) string {
	udta, haveUdta := findBox(f, moov.off, moovEnd, "udta")
	if haveUdta {
		if nam, ok := findBox(f, udta.off, udta.off+udta.size, namType); ok {
			if b, err := readAt(f, nam.off, min64(nam.size, maxTitleLen*4)); err == nil {
				if s := decodeText(b); s != "" {
					return s
				}
			}
		}
	}
	if meta, ok := findBox(f, moov.off, moovEnd, "meta"); ok {
		if s := ilstTitle(f, meta); s != "" {
			return s
		}
	}
	if haveUdta {
		if meta, ok := findBox(f, udta.off, udta.off+udta.size, "meta"); ok {
			if s := ilstTitle(f, meta); s != "" {
				return s
			}
		}
	}
	return ""
}

// ilstTitle extracts ©nam/data from an iTunes-style meta box.
func ilstTitle(f io.ReaderAt, meta boxLoc) string {
	// meta is a FullBox: its version/flags precede the children. A rare
	// QuickTime variant omits them — try both layouts.
	begin := meta.off + 4
	ilst, ok := findBox(f, begin, meta.off+meta.size, "ilst")
	if !ok {
		ilst, ok = findBox(f, meta.off, meta.off+meta.size, "ilst")
	}
	if !ok {
		return ""
	}
	nam, ok := findBox(f, ilst.off, ilst.off+ilst.size, namType)
	if !ok {
		return ""
	}
	data, ok := findBox(f, nam.off, nam.off+nam.size, "data")
	if !ok || data.size <= 8 {
		return ""
	}
	// data payload: version/flags + 4 reserved bytes, then the text.
	b, err := readAt(f, data.off+8, min64(data.size-8, maxTitleLen*4))
	if err != nil {
		return ""
	}
	return decodeText(b)
}

// readAt reads up to n bytes at off; a short read at EOF is fine.
func readAt(f io.ReaderAt, off int64, n int64) ([]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	b := make([]byte, n)
	m, err := f.ReadAt(b, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return b[:m], nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// decodeText cleans up a raw text payload: UTF-16 with BOM is decoded,
// invalid bytes are dropped, NUL padding and surrounding space trimmed.
// An empty or over-long result is treated as "no title".
func decodeText(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
		be := b[0] == 0xFE && b[1] == 0xFF
		u := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if be {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		b = []byte(string(utf16.Decode(u)))
	}
	s := strings.ToValidUTF8(string(b), "")
	s = strings.Trim(strings.TrimRight(s, "\x00"), " \t\r\n")
	if utf8.RuneCountInString(s) > maxTitleLen {
		s = string([]rune(s)[:maxTitleLen])
	}
	return s
}
