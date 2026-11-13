package httpd

import "testing"

const testSize = int64(4096)

func TestParseRangeSatisfiable(t *testing.T) {
	cases := []struct {
		header string
		start  int64
		end    int64
	}{
		{"bytes=0-1023", 0, 1023},
		{"bytes=1000-", 1000, 4095},   // open-ended
		{"bytes=-100", 3996, 4095},    // suffix: last 100 bytes
		{"bytes=0-0", 0, 0},           // one byte
		{"bytes=0-999999", 0, 4095},   // end clamped to size-1
		{"bytes=500-4000", 500, 4000}, // plain middle range
	}
	for _, tc := range cases {
		rng, ok, err := parseRange(tc.header, testSize)
		if err != nil || !ok {
			t.Errorf("parseRange(%q) = ok=%v err=%v, want ok=true err=nil", tc.header, ok, err)
			continue
		}
		if rng.start != tc.start || rng.end != tc.end {
			t.Errorf("parseRange(%q) = [%d,%d], want [%d,%d]", tc.header, rng.start, rng.end, tc.start, tc.end)
		}
	}
}

func TestParseRangeIgnore(t *testing.T) {
	// Absent, malformed, multi-range or invalid specs: answer 200.
	ignored := []string{
		"",
		"items=0-100",      // wrong unit
		"banana",           // not a range at all
		"bytes=banana",     // no dash
		"bytes=0-10,20-30", // multi-range: not worth multipart for VLC
		"bytes=10-5",       // end before start: invalid spec, ignored per RFC
		"bytes=-3-10",      // malformed
		"bytes=a-10",
		"bytes=10-a",
		"bytes=",
	}
	for _, header := range ignored {
		_, ok, err := parseRange(header, testSize)
		if ok || err != nil {
			t.Errorf("parseRange(%q) = ok=%v err=%v, want ok=false err=nil", header, ok, err)
		}
	}
}

func TestParseRangeUnsatisfiable(t *testing.T) {
	for _, header := range []string{
		"bytes=9999-", // start past EOF
		"bytes=4096-", // start == size is already past the end
		"bytes=-0",    // zero suffix length per RFC 7233 §2.1
	} {
		_, ok, err := parseRange(header, testSize)
		if ok || err == nil {
			t.Errorf("parseRange(%q) = ok=%v err=%v, want ok=false err=errUnsatisfiable", header, ok, err)
		}
	}

	// Any explicit range on an empty file is unsatisfiable.
	for _, header := range []string{"bytes=0-", "bytes=-5", "bytes=0-0"} {
		_, ok, err := parseRange(header, 0)
		if ok || err == nil {
			t.Errorf("parseRange(%q, 0) = ok=%v err=%v, want ok=false err=errUnsatisfiable", header, ok, err)
		}
	}
}
