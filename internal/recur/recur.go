// Package recur works out run times for recurring cards from an RFC 5545
// RRULE, a local start time and an IANA time zone. Times are computed in
// the zone, so a 9am card stays at 9am across daylight saving changes.
package recur

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// LocalLayout is the format of the local start time.
const LocalLayout = "2006-01-02T15:04:05"

// Rule is a parsed recurrence.
type Rule struct {
	r *rrule.RRule
}

// Parse checks and parses a rule. raw may start with "RRULE:". Weeks
// start on Monday unless the rule sets WKST.
func Parse(raw, dtstart, tz string) (*Rule, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		return nil, fmt.Errorf("unknown time zone %q", tz)
	}
	start, err := time.ParseInLocation(LocalLayout, strings.TrimSuffix(dtstart, "Z"), loc)
	if err != nil {
		// Accept a minute-precision value from a datetime-local input.
		start, err = time.ParseInLocation("2006-01-02T15:04", dtstart, loc)
		if err != nil {
			return nil, fmt.Errorf("dtstart must look like 2026-11-16T09:00:00")
		}
	}
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "RRULE:"))
	if raw == "" {
		return nil, errors.New("rrule is empty")
	}
	if strings.Contains(strings.ToUpper(raw), "DTSTART") || strings.ContainsAny(raw, "\n\r") {
		return nil, errors.New("put the start time in dtstart, not in the rule")
	}
	if !strings.Contains(strings.ToUpper(raw), "WKST=") {
		raw += ";WKST=MO"
	}
	opt, err := rrule.StrToROption(raw)
	if err != nil {
		return nil, fmt.Errorf("rrule: %w", err)
	}
	if opt.Freq == rrule.SECONDLY || opt.Freq == rrule.MINUTELY {
		return nil, errors.New("rules more frequent than hourly are not allowed")
	}
	opt.Dtstart = start
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, fmt.Errorf("rrule: %w", err)
	}
	return &Rule{r: r}, nil
}

// After returns the first run strictly after t, or the zero time if the
// rule has ended.
func (r *Rule) After(t time.Time) time.Time {
	n := r.r.After(t, false)
	if n.IsZero() {
		return n
	}
	return n.UTC()
}

// Next returns up to n runs after t.
func (r *Rule) Next(t time.Time, n int) []time.Time {
	var out []time.Time
	for len(out) < n {
		t = r.r.After(t, false)
		if t.IsZero() {
			break
		}
		out = append(out, t.UTC())
	}
	return out
}
