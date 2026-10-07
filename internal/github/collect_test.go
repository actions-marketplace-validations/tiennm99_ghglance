package github

import (
	"strings"
	"testing"
	"time"
)

// TestParseWeekday covers the common case-insensitive inputs and confirms
// an unknown value errors (the CLI then falls back to Sunday with a warn).
func TestParseWeekday(t *testing.T) {
	ok := []struct {
		in   string
		want time.Weekday
	}{
		{"", time.Sunday},
		{"sunday", time.Sunday},
		{"SUNDAY", time.Sunday},
		{"Sun", time.Sunday},
		{"  monday  ", time.Monday},
		{"Mon", time.Monday},
		{"saturday", time.Saturday},
	}
	for _, c := range ok {
		got, err := ParseWeekday(c.in)
		if err != nil {
			t.Errorf("ParseWeekday(%q) err=%v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseWeekday(%q)=%v want %v", c.in, got, c.want)
		}
	}
	if _, err := ParseWeekday("moonday"); err == nil {
		t.Error("ParseWeekday(moonday): want error, got nil")
	}
}

// TestUTCOffsetLabel checks the compact format: integer hours drop the
// minutes suffix, non-zero offsets render as `UTC±H:MM`.
func TestUTCOffsetLabel(t *testing.T) {
	cases := []struct {
		zone string
		want string // must appear in the label; exact value varies by DST
	}{
		{"UTC", "UTC+0"},
		{"Asia/Saigon", "UTC+7"},
		{"Asia/Kolkata", "UTC+5:30"},   // half-hour zone
		{"Asia/Kathmandu", "UTC+5:45"}, // quarter-hour zone
	}
	for _, tc := range cases {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Skipf("%s unavailable: %v", tc.zone, err)
		}
		got := UTCOffsetLabel(loc)
		if !strings.Contains(got, tc.want) {
			t.Errorf("UTCOffsetLabel(%q) = %q, want prefix %q", tc.zone, got, tc.want)
		}
	}
}

// TestScopesIncludeRepo checks that only the full "repo" scope marks a
// classic token as private-capable.
func TestScopesIncludeRepo(t *testing.T) {
	cases := map[string]bool{
		"":                          false,
		"read:user":                 false,
		"read:user, public_repo":    false,
		"repo:status, read:user":    false,
		"read:user, repo":           true,
		"repo":                      true,
		"gist,repo,workflow":        true,
		"read:org, read:user, repo": true,
	}
	for in, want := range cases {
		if got := scopesIncludeRepo(in); got != want {
			t.Errorf("scopesIncludeRepo(%q) = %v, want %v", in, got, want)
		}
	}
}
