//go:build linux && !rush_wpe

package rush

import "testing"

func TestX11RuneKeyNamesShiftedPunctuation(t *testing.T) {
	tests := []struct {
		character rune
		name      string
		shift     bool
	}{
		{character: '@', name: "at", shift: true},
		{character: '.', name: "period", shift: false},
		{character: '+', name: "plus", shift: true},
		{character: 'A', name: "A", shift: true},
		{character: 'a', name: "a", shift: false},
	}
	for _, test := range tests {
		name, shift := x11RuneKey(test.character)
		if name != test.name || shift != test.shift {
			t.Fatalf("x11RuneKey(%q) = %q, %t; want %q, %t", test.character, name, shift, test.name, test.shift)
		}
	}
}
