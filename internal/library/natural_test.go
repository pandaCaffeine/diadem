package library

import "testing"

func TestNaturalLess(t *testing.T) {
	less := [][2]string{
		{"S02E9.mkv", "S02E10.mkv"}, // the classic episode case
		{"file2.txt", "file10.txt"}, // plain numbers
		{"a1b2", "a1b10"},           // several digit runs
		{"b2", "b10"},
		{"episode04", "episode4x"},    // equal numbers, shorter first
		{"2 Broke Girls", "Big Bang"}, // digits before letters
		{"apple", "Banana"},           // case-insensitive
		{"abc", "abcd"},               // prefix first
		{"a01", "a2"},                 // leading zeros are not a bigger number
	}
	for _, c := range less {
		if !NaturalLess(c[0], c[1]) {
			t.Errorf("NaturalLess(%q, %q) = false, want true", c[0], c[1])
		}
		if NaturalLess(c[1], c[0]) {
			t.Errorf("NaturalLess(%q, %q) = true, want false", c[1], c[0])
		}
	}

	// Numerically equal strings must not be less in either direction.
	equal := [][2]string{{"a01", "a1"}, {"x007y", "x7y"}}
	for _, c := range equal {
		if NaturalLess(c[0], c[1]) || NaturalLess(c[1], c[0]) {
			t.Errorf("%q and %q should compare equal", c[0], c[1])
		}
	}
}
