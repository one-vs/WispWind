package paste

import "testing"

func TestNeedsLeadingSpace(t *testing.T) {
	cases := map[string]bool{
		"":        false,
		"  ":      false,
		"word":    true,
		"слово":   true,
		"42":      true,
		"end.":    true,
		"stop!":   true,
		"(":       false,
		"-":       false,
		"quote\"": true,
		"tail\n":  true, // trailing whitespace is trimmed before the check
	}
	for input, want := range cases {
		if got := needsLeadingSpace(input); got != want {
			t.Errorf("needsLeadingSpace(%q) = %v, want %v", input, got, want)
		}
	}
}
