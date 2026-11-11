package recur

import (
	"testing"
	"time"
)

func mustRule(t *testing.T, rule, start, tz string) *Rule {
	t.Helper()
	r, err := Parse(rule, start, tz)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A daily 9am rule keeps 9am local time across the DST changes in
// Sydney (first Sunday in April and October) and Auckland (first Sunday
// in April, last Sunday in September).
func TestDaylightSaving(t *testing.T) {
	for _, tc := range []struct{ tz, start string }{
		{"Australia/Sydney", "2026-04-03T09:00:00"},
		{"Australia/Sydney", "2026-10-02T09:00:00"},
		{"Pacific/Auckland", "2026-04-03T09:00:00"},
		{"Pacific/Auckland", "2026-09-25T09:00:00"},
	} {
		loc, _ := time.LoadLocation(tc.tz)
		r := mustRule(t, "FREQ=DAILY", tc.start, tc.tz)
		start, _ := time.ParseInLocation(LocalLayout, tc.start, loc)
		runs := r.Next(start.Add(-time.Second), 5)
		if len(runs) != 5 {
			t.Fatalf("%s: got %d runs", tc.tz, len(runs))
		}
		offsets := map[int]bool{}
		for _, run := range runs {
			local := run.In(loc)
			if local.Hour() != 9 || local.Minute() != 0 {
				t.Errorf("%s: run %v is not 9am local", tc.tz, local)
			}
			_, off := local.Zone()
			offsets[off] = true
		}
		if len(offsets) != 2 {
			t.Errorf("%s from %s: expected the runs to cross a DST change", tc.tz, tc.start)
		}
	}
}

func TestWeekStartsMonday(t *testing.T) {
	// Every second week on Monday and Sunday. With WKST=MO the Sunday
	// belongs to the same week as the Monday before it.
	r := mustRule(t, "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU", "2026-11-16T09:00:00", "Australia/Sydney")
	loc, _ := time.LoadLocation("Australia/Sydney")
	runs := r.Next(time.Date(2026, 11, 15, 0, 0, 0, 0, loc), 3)
	want := []string{"2026-11-16", "2026-11-22", "2026-11-30"}
	for i, w := range want {
		if got := runs[i].In(loc).Format("2006-01-02"); got != w {
			t.Errorf("run %d = %s, want %s", i, got, w)
		}
	}
}

func TestMonthlyNthWeekday(t *testing.T) {
	r := mustRule(t, "FREQ=MONTHLY;BYDAY=+2TU", "2026-11-01T10:30:00", "Pacific/Auckland")
	loc, _ := time.LoadLocation("Pacific/Auckland")
	got := r.After(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)).In(loc)
	if got.Format("2006-01-02 15:04") != "2026-11-10 10:30" {
		t.Fatalf("got %v", got)
	}
}

func TestRejects(t *testing.T) {
	for _, c := range [][3]string{
		{"FREQ=DAILY", "2026-01-01T09:00:00", "Mars/Base"},
		{"FREQ=MINUTELY", "2026-01-01T09:00:00", "UTC"},
		{"NOPE", "2026-01-01T09:00:00", "UTC"},
		{"FREQ=DAILY", "tomorrow", "UTC"},
		{"DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY", "2026-01-01T09:00:00", "UTC"},
	} {
		if _, err := Parse(c[0], c[1], c[2]); err == nil {
			t.Errorf("Parse(%q, %q, %q) should fail", c[0], c[1], c[2])
		}
	}
}
