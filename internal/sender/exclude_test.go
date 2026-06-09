package sender

import "testing"

func TestWildmatch(t *testing.T) {
	for _, tt := range []struct {
		pattern string
		text    string
		want    bool
	}{
		{"*.log", "foo.log", true},
		{"*.log", "foo.txt", false},
		{"*.log", "a.b.log", true},
		{"?.log", "a.log", true},
		{"?.log", "ab.log", false},
		{"foo", "foo", true},
		{"f*o", "fooo", true},
		{"f*o", "fox", false},
		// '*' must not cross '/', '**' may.
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"a/**", "a/b/c", true},
		{"*", "a/b", false},
		{"**", "a/b", true},
		// character classes
		{"[ab].txt", "a.txt", true},
		{"[ab].txt", "c.txt", false},
		{"[a-c].txt", "b.txt", true},
		{"[!a-c].txt", "d.txt", true},
		{"[!a-c].txt", "b.txt", false},
	} {
		if got := wildmatch(tt.pattern, tt.text); got != tt.want {
			t.Errorf("wildmatch(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}

func TestFilterRuleListMatches(t *testing.T) {
	// Exclude *.log, but an earlier include for keep.log wins (first match decides).
	l, err := ParseFilterRules([]string{"+ keep.log", "- *.log"})
	if err != nil {
		t.Fatal(err)
	}
	if !l.matches("foo.log") {
		t.Errorf("foo.log should be excluded")
	}
	if l.matches("keep.log") {
		t.Errorf("keep.log should be protected by the include rule")
	}
	if l.matches("foo.txt") {
		t.Errorf("foo.txt should not be excluded")
	}
	// A pattern with no slash matches by basename.
	if !l.matches("deep/nested/foo.log") {
		t.Errorf("basename match should exclude deep/nested/foo.log")
	}
}
