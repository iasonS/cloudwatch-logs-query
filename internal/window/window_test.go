package window

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func TestNoFlagsMeansTheLastHour(t *testing.T) {
	got, err := Flags{}.Resolve(now)
	if err != nil {
		t.Fatal(err)
	}
	want := Range{From: now.Add(-time.Hour), To: now}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSince(t *testing.T) {
	tests := map[string]time.Duration{
		"30m":   30 * time.Minute,
		"3h":    3 * time.Hour,
		"1h30m": 90 * time.Minute,
		"2d":    48 * time.Hour,
	}
	for since, back := range tests {
		got, err := Flags{Since: since}.Resolve(now)
		if err != nil {
			t.Fatalf("--since %s: %v", since, err)
		}
		want := Range{From: now.Add(-back), To: now}
		if got != want {
			t.Fatalf("--since %s: got %+v, want %+v", since, got, want)
		}
	}
}

func TestSinceRefusesWhatItCannotRead(t *testing.T) {
	for _, since := range []string{"abc", "3", "1d12h", "1.5d", "0", "0d", "-1h"} {
		if _, err := (Flags{Since: since}).Resolve(now); err == nil {
			t.Fatalf("--since %s was accepted", since)
		}
	}
}

func TestFromAndTo(t *testing.T) {
	got, err := Flags{From: "2026-01-05T10:00Z", To: "2026-01-05T12:30:15+01:00"}.Resolve(now)
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 1, 5, 11, 30, 15, 0, time.UTC)
	if !got.From.Equal(wantFrom) || !got.To.Equal(wantTo) {
		t.Fatalf("got %+v, want %s to %s", got, wantFrom, wantTo)
	}
}

func TestFromAloneEndsNow(t *testing.T) {
	got, err := Flags{From: "2026-11-19T10:00:00Z"}.Resolve(now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.To.Equal(now) {
		t.Fatalf("got end %s, want now", got.To)
	}
}

func TestTimeWithoutZoneIsRefused(t *testing.T) {
	for _, from := range []string{"2026-01-05T10:00", "2026-01-05T10:00:00", "2026-01-05T10:00:00.5"} {
		_, err := Flags{From: from}.Resolve(now)
		if err == nil || !strings.Contains(err.Error(), "no time zone") {
			t.Fatalf("--from %s: got %v, want a no time zone error", from, err)
		}
	}
}

func TestUnreadableTimeIsRefused(t *testing.T) {
	_, err := Flags{From: "yesterday"}.Resolve(now)
	if err == nil || !strings.Contains(err.Error(), "is not a time") {
		t.Fatalf("got %v, want an is not a time error", err)
	}
}

func TestToNeedsFrom(t *testing.T) {
	if _, err := (Flags{To: "2026-01-05T10:00Z"}).Resolve(now); err == nil {
		t.Fatal("--to without --from was accepted")
	}
}

func TestSinceAndFromTogetherAreRefused(t *testing.T) {
	if _, err := (Flags{Since: "3h", From: "2026-01-05T10:00Z"}).Resolve(now); err == nil {
		t.Fatal("--since with --from was accepted")
	}
}

func TestFromMustBeBeforeTo(t *testing.T) {
	if _, err := (Flags{From: "2026-01-05T11:00Z", To: "2026-01-05T10:00Z"}).Resolve(now); err == nil {
		t.Fatal("--from after --to was accepted")
	}
	if _, err := (Flags{From: "2026-11-20T10:00Z"}).Resolve(now); err == nil {
		t.Fatal("--from in the future without --to was accepted")
	}
}
