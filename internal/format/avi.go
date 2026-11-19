package format

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

// readAVI parses a RIFF AVI file. Duration comes from the avih header:
// frame count × microseconds per frame. For OpenDML (Type 2, VBR) files
// the real frame count lives in the dmlh extension, which overrides
// avih's. The title is the INAM chunk of the INFO list.
func readAVI(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return Meta{}, err
	}

	hdr, err := readAt(f, 0, 12)
	if err != nil || len(hdr) < 12 ||
		string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "AVI " {
		return Meta{}, errors.New("not an AVI file (missing RIFF/AVID)")
	}
	// The RIFF size field is a uint32 that occasionally lies on huge
	// files; never walk past the real end.
	riffEnd := min64(end, int64(binary.LittleEndian.Uint32(hdr[4:8]))+8)

	var (
		usecPerFrame uint32
		totalFrames  uint32
		haveAvih     bool
		title        string
	)

	off := int64(12)
	for i := 0; off+8 <= riffEnd && i < boxLimit; i++ {
		ch, ok := readChunk(f, off, riffEnd)
		if !ok {
			break
		}
		if ch.cc == "LIST" && ch.size >= 4 {
			listType, err := readAt(f, ch.dataOff, 4)
			if err == nil {
				switch string(listType) {
				case "hdrl":
					usecPerFrame, totalFrames, haveAvih = aviHdrl(f, ch.dataOff+4, ch.dataOff+ch.size)
				case "INFO":
					title = aviInfoTitle(f, ch.dataOff+4, ch.dataOff+ch.size)
				}
			}
		}
		off = ch.next
	}
	if !haveAvih {
		return Meta{}, errors.New("no avih header found")
	}
	if usecPerFrame == 0 || totalFrames == 0 {
		return Meta{Title: title}, nil // header says zero frames: unknown duration
	}
	meta := Meta{Title: title}
	// The multiplication fits uint64 ((2³²−1)² < 2⁶⁴) but the
	// nanosecond form would overflow int64 and could wrap into a
	// plausible lie; clamp to the same 24h ceiling probe.go trusts.
	usec := uint64(totalFrames) * uint64(usecPerFrame)
	if usec > uint64(maxAVIDuration/time.Microsecond) {
		return meta, nil // over a day long: implausible, treat as unknown
	}
	meta.Duration = time.Duration(usec) * time.Microsecond
	return meta, nil
}

// maxAVIDuration mirrors the plausibility ceiling of library/probe.go,
// which format cannot import: durations past it are dropped there, so
// they are dropped here before they can overflow.
const maxAVIDuration = 24 * time.Hour

// aviHdrl walks the hdrl list: avih carries microsec-per-frame and the
// frame count, the odml/dmlh list carries the real count for OpenDML.
func aviHdrl(f io.ReaderAt, begin, end int64) (usecPerFrame, totalFrames uint32, ok bool) {
	off := begin
	for i := 0; off+8 <= end && i < boxLimit; i++ {
		ch, chOk := readChunk(f, off, end)
		if !chOk {
			break
		}
		switch ch.cc {
		case "avih":
			if b, err := readAt(f, ch.dataOff, min64(ch.size, 64)); err == nil && len(b) >= 20 {
				usecPerFrame = binary.LittleEndian.Uint32(b[0:4])
				totalFrames = binary.LittleEndian.Uint32(b[16:20])
				ok = true
			}
		case "LIST":
			if t, err := readAt(f, ch.dataOff, 4); err == nil && string(t) == "odml" {
				if n := aviDmlhFrames(f, ch.dataOff+4, ch.dataOff+ch.size); n != 0 {
					totalFrames = n
					ok = true
				}
			}
		}
		off = ch.next
	}
	return usecPerFrame, totalFrames, ok
}

// aviDmlhFrames reads the OpenDML extended frame count from dmlh.
func aviDmlhFrames(f io.ReaderAt, begin, end int64) uint32 {
	off := begin
	for i := 0; off+8 <= end && i < boxLimit; i++ {
		ch, ok := readChunk(f, off, end)
		if !ok {
			break
		}
		if ch.cc == "dmlh" {
			if b, err := readAt(f, ch.dataOff, min64(ch.size, 8)); err == nil && len(b) >= 4 {
				return binary.LittleEndian.Uint32(b[0:4])
			}
		}
		off = ch.next
	}
	return 0
}

// aviInfoTitle picks INAM out of the INFO list.
func aviInfoTitle(f io.ReaderAt, begin, end int64) string {
	off := begin
	for i := 0; off+8 <= end && i < boxLimit; i++ {
		ch, ok := readChunk(f, off, end)
		if !ok {
			break
		}
		if ch.cc == "INAM" {
			if b, err := readAt(f, ch.dataOff, min64(ch.size, maxTitleLen*4)); err == nil {
				return decodeText(b)
			}
		}
		off = ch.next
	}
	return ""
}

// chunk is one RIFF chunk: a fourcc, a little-endian 32-bit size, then
// the payload. RIFF pads payloads to even sizes; next accounts for it.
type chunkLoc struct {
	cc      string
	dataOff int64
	size    int64
	next    int64
}

func readChunk(f io.ReaderAt, off, end int64) (chunkLoc, bool) {
	h, err := readAt(f, off, 8)
	if err != nil || len(h) < 8 {
		return chunkLoc{}, false
	}
	size := int64(binary.LittleEndian.Uint32(h[4:8]))
	c := chunkLoc{
		cc:      string(h[0:4]),
		dataOff: off + 8,
		size:    size,
	}
	c.next = c.dataOff + size
	if c.next > end || c.next < c.dataOff {
		return chunkLoc{}, false
	}
	if c.next&1 == 1 {
		c.next++ // RIFF chunks are word-aligned
	}
	return c, true
}
