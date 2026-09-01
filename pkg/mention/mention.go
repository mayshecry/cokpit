package mention

import (
	"sort"
	"strings"
	"unicode"
)

const Prefix = '@'

func Parse(body string) []string {
	var names []string
	for i, seg := range splitCodeSpans(body) {
		if i%2 == 1 {
			continue
		}
		names = append(names, parseSegment(seg)...)
	}
	return dedupe(names)
}

func splitCodeSpans(body string) []string {
	return strings.Split(body, "`")
}

func parseSegment(seg string) []string {
	var names []string
	runes := []rune(seg)
	for i := 0; i < len(runes); i++ {
		if runes[i] != Prefix {
			continue
		}
		if i > 0 && isNameRune(runes[i-1]) {
			continue
		}
		j := i + 1
		for j < len(runes) && isNameRune(runes[j]) {
			j++
		}
		name := strings.TrimRight(string(runes[i+1:j]), ".")
		if valid(name) {
			names = append(names, name)
		}
		i = j - 1
	}
	return names
}

func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-'
}

func valid(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !isNameRune(r) {
			return false
		}
	}
	return true
}

func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func Resolve(names []string, known map[string]bool) []string {
	var out []string
	for _, n := range names {
		if known[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
