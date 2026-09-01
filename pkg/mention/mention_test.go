package mention

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBasic(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"single", "ping @alice please", []string{"alice"}},
		{"multiple sorted dedup", "@bob and @alice and @bob again", []string{"alice", "bob"}},
		{"at start", "@alice hi", []string{"alice"}},
		{"at end", "check with @qc_lead", []string{"qc_lead"}},
		{"punctuation delimiters", "hey @alice, @bob. @carol!", []string{"alice", "bob", "carol"}},
		{"digits dashes dots", "@user-2.x is on it", []string{"user-2.x"}},
		{"empty", "no mentions here", []string{}},
		{"bare at", "hi @ and @ alone", []string{}},
		{"trailing at", "ship it @", []string{}},
		{"email not a mention", "contact bob@example.com", []string{}},
		{"adjacent ats", "@@alice", []string{"alice"}},
		{"unicode letters", "@müller review", []string{"müller"}},
		{"name too long ignored", "@" + strings.Repeat("a", 65), []string{}},
		{"max length ok", "@" + strings.Repeat("b", 64), []string{strings.Repeat("b", 64)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestParseCodeSpansExcluded(t *testing.T) {
	got := Parse("@alice `@bob` @carol")
	want := []string{"alice", "carol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %v, want %v", got, want)
	}

	got = Parse("@a `code @b` @c `@d` @e")
	want = []string{"a", "c", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse multi-span = %v, want %v", got, want)
	}
}

func TestParseSmokeBody(t *testing.T) {
	got := Parse("ship it @admin and @ghost1788180413 <script>alert(1)</script>")
	want := []string{"admin", "ghost1788180413"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(smoke body) = %v, want %v", got, want)
	}
}

func TestResolve(t *testing.T) {
	known := map[string]bool{"alice": true, "carol": true}
	got := Resolve([]string{"alice", "bob", "carol"}, known)
	want := []string{"alice", "carol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve = %v, want %v", got, want)
	}
	if got := Resolve(nil, known); got != nil {
		t.Errorf("Resolve(nil) = %v, want nil", got)
	}
}
