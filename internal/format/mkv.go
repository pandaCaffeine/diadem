package format

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"time"
)

// EBML element ids used by the Matroska parser.
const (
	idEBMLHeader    = 0x1A45DFA3
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idTitle         = 0x7BA9
)

// defaultTimecodeScale is the EBML default: Duration ticks are in
// units of 1'000'000 nanoseconds unless Info says otherwise.
const defaultTimecodeScale = 1_000_000

// elemLimit bounds how many sibling elements are walked per level.
const elemLimit = 4096

// elem is one EBML element located in a file.
type elem struct {
	id       uint32
	dataOff  int64
	dataSize int64 // -1 when the size is the "unknown" vint
	next     int64 // where the next sibling element starts
}

// readElem reads one EBML element header at off: an id vint followed by
// a data-size vint.
func readElem(f io.ReaderAt, off, end int64) (e elem, ok bool) {
	id, idLen, _, ok := readVINT(f, off, end, true)
	if !ok || id > math.MaxUint32 {
		return e, false
	}
	size, sizeLen, unknown, ok := readVINT(f, off+int64(idLen), end, false)
	if !ok {
		return e, false
	}
	e.id = uint32(id)
	e.dataOff = off + int64(idLen+sizeLen)
	if unknown {
		e.dataSize = -1
		e.next = end // best effort: an unknown-size element owns the rest
	} else {
		e.dataSize = int64(size)
		e.next = e.dataOff + e.dataSize
		if e.next > end {
			return e, false
		}
	}
	return e, true
}

// readVINT reads one EBML variable-size integer at off. The length is
// the position of the first set bit of the first byte. For ids the
// marker bit stays part of the value; for sizes it is stripped, and a
// value of all ones means "unknown". The returned length covers both
// vints' bytes; valueOffset is not needed because callers read the data
// themselves.
func readVINT(f io.ReaderAt, off, end int64, id bool) (value uint64, length int, unknown bool, ok bool) {
	var b [8]byte
	if off < 0 || off+1 > end {
		return 0, 0, false, false
	}
	if _, err := f.ReadAt(b[:1], off); err != nil {
		return 0, 0, false, false
	}
	first := b[0]
	if first == 0 {
		return 0, 0, false, false // no marker bit: not a valid vint
	}
	length = leadingZeros(first) + 1
	if off+int64(length) > end {
		return 0, 0, false, false
	}
	if _, err := f.ReadAt(b[:length], off); err != nil {
		return 0, 0, false, false
	}
	value = 0
	for i := 0; i < length; i++ {
		value = value<<8 | uint64(b[i])
	}
	if id {
		return value, length, false, true
	}
	dataBits := uint(7 * length)
	mask := uint64(1)<<dataBits - 1
	data := value & mask
	if data == mask {
		return 0, length, true, true // all ones: unknown size
	}
	return data, length, false, true
}

func leadingZeros(b byte) int {
	n := 0
	for i := 7; i >= 0 && b&(1<<uint(i)) == 0; i-- {
		n++
	}
	return n
}

// readMKV parses a Matroska file (.mkv, .webm): an EBML header, then a
// Segment whose Info element carries the title, the timecode scale and
// the duration. SeekHead, Cues and Clusters are skipped by size without
// reading their bodies.
func readMKV(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return Meta{}, err
	}
	if end < 4 {
		return Meta{}, errors.New("file too small for Matroska")
	}

	off := int64(0)
	if el, ok := readElem(f, off, end); ok && el.id == idEBMLHeader {
		off = el.next
	}
	for i := 0; off < end && i < elemLimit; i++ {
		el, ok := readElem(f, off, end)
		if !ok {
			break
		}
		if el.id == idSegment {
			return matroskaSegment(f, el, end)
		}
		if el.dataSize < 0 {
			break // unknown-size element before Segment: nothing useful beyond
		}
		off = el.next
	}
	return Meta{}, errors.New("no Segment element found: not a Matroska file")
}

// matroskaSegment finds the Info element among the Segment's children.
func matroskaSegment(f io.ReaderAt, seg elem, fileEnd int64) (Meta, error) {
	childEnd := fileEnd
	if seg.dataSize >= 0 {
		childEnd = seg.dataOff + seg.dataSize
	}
	off := seg.dataOff
	for i := 0; off < childEnd && i < elemLimit; i++ {
		el, ok := readElem(f, off, childEnd)
		if !ok {
			break
		}
		if el.id == idInfo {
			return matroskaInfo(f, el, childEnd)
		}
		if el.dataSize < 0 {
			break // e.g. a live stream's unknown-size Cluster: no Info follows
		}
		off = el.next
	}
	return Meta{}, errors.New("no Info element found in Segment")
}

// matroskaInfo decodes TimecodeScale, Duration and Title.
func matroskaInfo(f io.ReaderAt, info elem, parentEnd int64) (Meta, error) {
	end := parentEnd
	if info.dataSize >= 0 {
		end = info.dataOff + info.dataSize
	}
	scale := int64(defaultTimecodeScale)
	var ticks float64
	var haveTicks bool

	meta := Meta{}
	off := info.dataOff
	for i := 0; off < end && i < elemLimit; i++ {
		el, ok := readElem(f, off, end)
		if !ok {
			break
		}
		switch el.id {
		case idTimecodeScale:
			if b, err := readAt(f, el.dataOff, min64(el.dataSize, 8)); err == nil && len(b) > 0 {
				var v int64
				for _, c := range b {
					v = v<<8 | int64(c)
				}
				if v > 0 {
					scale = v
				}
			}
		case idDuration:
			b, err := readAt(f, el.dataOff, min64(el.dataSize, 8))
			if err == nil && len(b) == 4 {
				ticks = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
				haveTicks = true
			} else if err == nil && len(b) == 8 {
				ticks = math.Float64frombits(binary.BigEndian.Uint64(b))
				haveTicks = true
			}
		case idTitle:
			if b, err := readAt(f, el.dataOff, min64(el.dataSize, maxTitleLen*4)); err == nil {
				meta.Title = decodeText(b)
			}
		}
		if el.dataSize < 0 {
			break
		}
		off = el.next
	}

	if haveTicks && ticks > 0 && !math.IsNaN(ticks) && !math.IsInf(ticks, 0) {
		ns := ticks * float64(scale)
		if ns >= 1 && ns < float64(1<<62) { // keep far from overflowing time.Duration
			meta.Duration = time.Duration(ns)
		}
	}
	return meta, nil
}
