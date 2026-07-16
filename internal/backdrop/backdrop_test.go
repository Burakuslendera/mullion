package backdrop

import "testing"

// TestParseColourAcceptsOnlySixHexDigits locks the command's whole input
// surface: exactly #rrggbb or rrggbb, case-insensitive, nothing else. The
// rejects matter more than the accepts - the parse is strict so that a typo'd
// or hostile argument becomes a clean error, never a guess.
func TestParseColourAcceptsOnlySixHexDigits(t *testing.T) {
	accepted := map[string]Colour{
		"#2b2d34": {R: 0x2b, G: 0x2d, B: 0x34},
		"2b2d34":  {R: 0x2b, G: 0x2d, B: 0x34},
		"#2B2D34": {R: 0x2b, G: 0x2d, B: 0x34},
		"#ffffff": {R: 0xff, G: 0xff, B: 0xff},
		"000000":  {},
	}
	for value, want := range accepted {
		got, err := ParseColour(value)
		if err != nil {
			t.Errorf("ParseColour(%q) rejected a valid colour: %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("ParseColour(%q) = %+v, want %+v", value, got, want)
		}
	}

	rejected := []string{
		"", "#", "#fff", "fff", "#2b2d344", "#2b2d3", "2b2d344",
		"#gggggg", "#2b 2d34", " #2b2d34", "#2b2d34 ", "##2b2d3",
		"rgb(1,2,3)", "javascript:x",
	}
	for _, value := range rejected {
		if _, err := ParseColour(value); err == nil {
			t.Errorf("ParseColour(%q) accepted an invalid colour", value)
		}
	}
}

// TestDefaultHexParses pins that the default the command ships is itself a
// valid input - a default that fails its own parse would be a command that
// cannot run bare.
func TestDefaultHexParses(t *testing.T) {
	colour, err := ParseColour(DefaultHex)
	if err != nil {
		t.Fatalf("DefaultHex %q does not parse: %v", DefaultHex, err)
	}
	if (colour == Colour{}) {
		t.Fatalf("DefaultHex parsed to black; the documented default is a dark grey")
	}
}
