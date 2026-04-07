package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpr is a parsed five-field cron expression.
//
// Format: "minute hour day-of-month month day-of-week" where each field
// supports: a single integer, the wildcard "*", a comma-separated list of
// integers, an inclusive range "a-b", and a step "*/n" or "a-b/n".
//
// Day-of-week uses 0-6 with 0 = Sunday. Month uses 1-12. Day-of-month uses
// 1-31. Hour uses 0-23. Minute uses 0-59.
//
// This implementation deliberately covers only the standard 5-field POSIX
// dialect; named aliases like "@daily" and seconds-precision are not
// supported. The scheduler ticks once per minute, so finer granularity is
// not meaningful here either.
type CronExpr struct {
	minutes    []int // sorted, unique values within range
	hours      []int
	daysOfMon  []int
	months     []int
	daysOfWeek []int
}

// ErrInvalidCron is returned when a cron expression cannot be parsed.
var ErrInvalidCron = errors.New("invalid cron expression")

// ParseCron parses a 5-field cron expression. Returns ErrInvalidCron with
// a wrapped descriptive error on failure.
func ParseCron(expr string) (*CronExpr, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("scheduler.ParseCron: %w: empty", ErrInvalidCron)
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("scheduler.ParseCron: %w: expected 5 fields, got %d", ErrInvalidCron, len(fields))
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("scheduler.ParseCron: minute: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("scheduler.ParseCron: hour: %w", err)
	}
	daysOfMon, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("scheduler.ParseCron: day-of-month: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("scheduler.ParseCron: month: %w", err)
	}
	daysOfWeek, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("scheduler.ParseCron: day-of-week: %w", err)
	}

	return &CronExpr{
		minutes:    minutes,
		hours:      hours,
		daysOfMon:  daysOfMon,
		months:     months,
		daysOfWeek: daysOfWeek,
	}, nil
}

// Next returns the smallest time strictly greater than after at which this
// cron expression should fire, computed in after's location. The result is
// minute-aligned (seconds and nanoseconds are zero).
//
// The classic cron quirk applies: when both day-of-month and day-of-week
// are restricted (neither is "*"), a date matches if either field matches
// (OR semantics, not AND).
func (c *CronExpr) Next(after time.Time) time.Time {
	// Step to the next minute boundary so we never return `after` itself.
	t := after.Add(time.Minute).Truncate(time.Minute)

	dayOfMonRestricted := len(c.daysOfMon) != 31
	dayOfWeekRestricted := len(c.daysOfWeek) != 7

	// Bound the search: 5 years is well beyond any practical cron schedule
	// and prevents an infinite loop on malformed expressions.
	limit := after.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !contains(c.months, int(t.Month())) {
			// Jump to the first day of the next month at 00:00.
			year, month := t.Year(), t.Month()
			t = time.Date(year, month+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		dayMatches := false
		if dayOfMonRestricted && dayOfWeekRestricted {
			dayMatches = contains(c.daysOfMon, t.Day()) || contains(c.daysOfWeek, int(t.Weekday()))
		} else if dayOfMonRestricted {
			dayMatches = contains(c.daysOfMon, t.Day())
		} else if dayOfWeekRestricted {
			dayMatches = contains(c.daysOfWeek, int(t.Weekday()))
		} else {
			dayMatches = true
		}

		if !dayMatches {
			// Jump to the next day at 00:00.
			next := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			t = next.Add(24 * time.Hour)
			continue
		}

		if !contains(c.hours, t.Hour()) {
			// Jump to the next hour at minute 0.
			next := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
			t = next.Add(time.Hour)
			continue
		}

		if !contains(c.minutes, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}

		return t
	}
	return time.Time{}
}

// parseField parses one cron field against the inclusive range [lo, hi].
func parseField(field string, lo, hi int) ([]int, error) {
	if field == "" {
		return nil, fmt.Errorf("%w: empty field", ErrInvalidCron)
	}

	// A field is a comma-separated list of subfields, each of which may
	// be a single value, a range, or a stepped range.
	seen := make(map[int]struct{})
	for _, part := range strings.Split(field, ",") {
		values, err := parseSubfield(part, lo, hi)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			seen[v] = struct{}{}
		}
	}

	// Sort for deterministic Next() walks and easier debugging.
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sortInts(out)
	return out, nil
}

// parseSubfield parses one comma-separated component of a field.
func parseSubfield(part string, lo, hi int) ([]int, error) {
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		stepStr := part[idx+1:]
		s, err := strconv.Atoi(stepStr)
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("%w: invalid step %q", ErrInvalidCron, stepStr)
		}
		step = s
		part = part[:idx]
	}

	var rangeLo, rangeHi int
	switch {
	case part == "*":
		rangeLo, rangeHi = lo, hi
	case strings.Contains(part, "-"):
		bounds := strings.SplitN(part, "-", 2)
		a, err := strconv.Atoi(bounds[0])
		if err != nil {
			return nil, fmt.Errorf("%w: invalid range start %q", ErrInvalidCron, bounds[0])
		}
		b, err := strconv.Atoi(bounds[1])
		if err != nil {
			return nil, fmt.Errorf("%w: invalid range end %q", ErrInvalidCron, bounds[1])
		}
		if a > b {
			return nil, fmt.Errorf("%w: range start %d > end %d", ErrInvalidCron, a, b)
		}
		rangeLo, rangeHi = a, b
	default:
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid value %q", ErrInvalidCron, part)
		}
		rangeLo, rangeHi = v, v
	}

	if rangeLo < lo || rangeHi > hi {
		return nil, fmt.Errorf("%w: value out of bounds [%d,%d]", ErrInvalidCron, lo, hi)
	}

	var out []int
	for v := rangeLo; v <= rangeHi; v += step {
		out = append(out, v)
	}
	return out, nil
}

func contains(xs []int, target int) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func sortInts(xs []int) {
	// Simple insertion sort: cron field cardinalities are tiny (<= 60).
	for i := 1; i < len(xs); i++ {
		v := xs[i]
		j := i - 1
		for j >= 0 && xs[j] > v {
			xs[j+1] = xs[j]
			j--
		}
		xs[j+1] = v
	}
}
