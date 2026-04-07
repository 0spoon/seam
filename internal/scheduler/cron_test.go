package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseCron_Valid(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"every minute", "* * * * *"},
		{"specific hour and minute", "30 9 * * *"},
		{"step minutes", "*/15 * * * *"},
		{"weekday range", "0 8 * * 1-5"},
		{"comma list", "0,15,30,45 * * * *"},
		{"day-of-month range with step", "0 0 1-15/2 * *"},
		{"every weekday at 09:30", "30 9 * * 1,2,3,4,5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCron(tc.expr)
			require.NoError(t, err, "expr=%q", tc.expr)
		})
	}
}

func TestParseCron_Invalid(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"too few fields", "* * * *"},
		{"too many fields", "* * * * * *"},
		{"invalid minute", "60 * * * *"},
		{"invalid hour", "0 24 * * *"},
		{"invalid day", "0 0 32 * *"},
		{"invalid month", "0 0 1 13 *"},
		{"invalid day-of-week", "0 0 * * 7"},
		{"reverse range", "0 0 5-1 * *"},
		{"non-numeric", "abc * * * *"},
		{"zero step", "*/0 * * * *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCron(tc.expr)
			require.Error(t, err, "expr=%q should fail", tc.expr)
		})
	}
}

func TestCronExpr_Next_EveryMinute(t *testing.T) {
	expr, err := ParseCron("* * * * *")
	require.NoError(t, err)

	start := time.Date(2026, 4, 6, 12, 30, 15, 0, time.UTC)
	next := expr.Next(start)

	// Next minute boundary after 12:30:15 is 12:31:00.
	require.Equal(t, time.Date(2026, 4, 6, 12, 31, 0, 0, time.UTC), next)
}

func TestCronExpr_Next_DailyAt8(t *testing.T) {
	expr, err := ParseCron("0 8 * * *")
	require.NoError(t, err)

	// Same day before 08:00 -> today at 08:00.
	start := time.Date(2026, 4, 6, 7, 0, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC), expr.Next(start))

	// Same day after 08:00 -> tomorrow at 08:00.
	start = time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC), expr.Next(start))

	// Exactly 08:00 -> tomorrow (Next is strictly after `after`).
	start = time.Date(2026, 4, 6, 8, 0, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 7, 8, 0, 0, 0, time.UTC), expr.Next(start))
}

func TestCronExpr_Next_Weekday(t *testing.T) {
	// 09:30 every weekday (Mon-Fri).
	expr, err := ParseCron("30 9 * * 1-5")
	require.NoError(t, err)

	// Friday 2026-04-03 10:00 -> Monday 2026-04-06 09:30.
	start := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 6, 9, 30, 0, 0, time.UTC), expr.Next(start))

	// Saturday 2026-04-04 09:30 -> Monday 2026-04-06 09:30.
	start = time.Date(2026, 4, 4, 9, 30, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 6, 9, 30, 0, 0, time.UTC), expr.Next(start))
}

func TestCronExpr_Next_StepMinutes(t *testing.T) {
	// Every 15 minutes.
	expr, err := ParseCron("*/15 * * * *")
	require.NoError(t, err)

	start := time.Date(2026, 4, 6, 12, 7, 0, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 6, 12, 15, 0, 0, time.UTC), expr.Next(start))

	start = time.Date(2026, 4, 6, 12, 45, 30, 0, time.UTC)
	require.Equal(t, time.Date(2026, 4, 6, 13, 0, 0, 0, time.UTC), expr.Next(start))
}

func TestCronExpr_Next_DayOrSemantics(t *testing.T) {
	// Both day-of-month (15) and day-of-week (Sunday=0) restricted ->
	// classic cron OR semantics.
	expr, err := ParseCron("0 0 15 * 0")
	require.NoError(t, err)

	// 2026-04-12 is a Sunday -- should fire even though it's not the 15th.
	start := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	got := expr.Next(start)
	require.Equal(t, time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC), got)
}
