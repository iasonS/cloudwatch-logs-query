// Package window turns the --since, --from and --to flags into the start and end of a query.
package window

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultSince is how far back a query looks when no time flag is given.
const defaultSince = time.Hour

// zonedLayouts are RFC 3339 and the same without seconds, as in 2026-01-05T10:00Z.
var zonedLayouts = []string{time.RFC3339, "2006-01-02T15:04Z07:00"}

// unzonedLayouts are only used to explain why a time without a zone is refused.
var unzonedLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04"}

// Flags are the time flags as typed; an empty string means the flag was not given.
type Flags struct {
	Since string // how far back from now, such as 30m, 3h or 2d
	From  string // RFC 3339 with a zone
	To    string // RFC 3339 with a zone; empty means now
}

// Range is the time a query looks at.
type Range struct {
	From time.Time
	To   time.Time
}

// Resolve returns the range the flags ask for. With no flags it is the last hour.
func (f Flags) Resolve(now time.Time) (Range, error) {
	if f.From == "" && f.To == "" {
		return f.lookBack(now)
	}
	if f.Since != "" {
		return Range{}, errors.New("use --since or --from/--to, not both")
	}
	return f.between(now)
}

func (f Flags) lookBack(now time.Time) (Range, error) {
	since := defaultSince
	if f.Since != "" {
		parsed, err := parseSince(f.Since)
		if err != nil {
			return Range{}, err
		}
		since = parsed
	}
	return Range{From: now.Add(-since), To: now}, nil
}

func (f Flags) between(now time.Time) (Range, error) {
	if f.From == "" {
		return Range{}, errors.New("--to needs --from")
	}
	from, err := parseTime("--from", f.From)
	if err != nil {
		return Range{}, err
	}

	to := now
	if f.To != "" {
		to, err = parseTime("--to", f.To)
		if err != nil {
			return Range{}, err
		}
	}

	if !from.Before(to) {
		return Range{}, errors.New("--from must be before --to (or before now, when --to is not given)")
	}
	return Range{From: from, To: to}, nil
}

func parseSince(text string) (time.Duration, error) {
	since, err := parseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("--since %s: use a duration such as 30m, 3h or 2d", text)
	}
	if since <= 0 {
		return 0, fmt.Errorf("--since %s: must be more than zero", text)
	}
	return since, nil
}

// parseDuration reads a Go duration such as 90m or 3h, or whole days such as 2d, which Go
// durations do not have.
func parseDuration(text string) (time.Duration, error) {
	days, isDays := strings.CutSuffix(text, "d")
	if !isDays {
		return time.ParseDuration(text)
	}
	count, err := strconv.Atoi(days)
	if err != nil {
		return 0, err
	}
	return time.Duration(count) * 24 * time.Hour, nil
}

func parseTime(flag, text string) (time.Time, error) {
	for _, layout := range zonedLayouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, nil
		}
	}
	if matchesAny(unzonedLayouts, text) {
		return time.Time{}, fmt.Errorf("%s %s has no time zone; add Z for UTC or an offset such as +01:00", flag, text)
	}
	return time.Time{}, fmt.Errorf("%s %s is not a time such as 2026-01-05T10:00Z", flag, text)
}

func matchesAny(layouts []string, text string) bool {
	for _, layout := range layouts {
		if _, err := time.Parse(layout, text); err == nil {
			return true
		}
	}
	return false
}
