// Package notify holds the pure rules for in-app notifications: who is
// mentioned in some text, and whether a user wants a kind of notice.
// The service layer writes the rows. An email sender can be added later
// by reading notifications that have not been emailed.
package notify

import (
	"encoding/json"
	"regexp"
	"strings"
)

var mentionRe = regexp.MustCompile(`(?:^|[^\w@])@([A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9]|[A-Za-z0-9])`)

// Mentions returns the lower-cased handles mentioned in text, in order,
// without duplicates. Code spans and blocks are skipped.
func Mentions(text string) []string {
	text = stripCode(text)
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		h := strings.ToLower(m[1])
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

var codeRe = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")

func stripCode(s string) string { return codeRe.ReplaceAllString(s, " ") }

// Person is the part of a user that mention matching needs.
type Person struct {
	ID          string
	Email       string
	DisplayName string
}

// Handle is the mention handle for a person: the local part of their
// email, or their display name without spaces.
func Handle(p Person) string {
	if i := strings.IndexByte(p.Email, '@'); i > 0 {
		return strings.ToLower(p.Email[:i])
	}
	return strings.ToLower(strings.ReplaceAll(p.DisplayName, " ", ""))
}

// Match returns the IDs of people whose handle, or display name without
// spaces, matches a mention.
func Match(handles []string, people []Person) []string {
	want := map[string]bool{}
	for _, h := range handles {
		want[h] = true
	}
	var out []string
	for _, p := range people {
		if want[Handle(p)] || want[strings.ToLower(strings.ReplaceAll(p.DisplayName, " ", ""))] {
			out = append(out, p.ID)
		}
	}
	return out
}

// Prefs is a user's per-trigger settings. Missing keys mean on.
type Prefs map[string]bool

func ParsePrefs(raw string) Prefs {
	p := Prefs{}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// On reports whether a trigger is on.
func (p Prefs) On(key string) bool {
	v, ok := p[key]
	return !ok || v
}
