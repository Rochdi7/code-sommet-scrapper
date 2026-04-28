package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSchedule parses a simple schedule expression and returns the next run time.
// Supported formats:
//   - "every 1h", "every 30m", "every 24h" (interval-based, from now)
//   - "daily 09:00", "daily 14:30" (daily at specific time)
//   - "weekly mon 09:00", "weekly fri 14:00" (weekly on day at time)
func ParseSchedule(expr string) (time.Time, error) {
	return parseScheduleFrom(expr, time.Now().UTC())
}

// ParseScheduleFrom computes the next run time relative to the given reference time.
func ParseScheduleFrom(expr string, from time.Time) (time.Time, error) {
	return parseScheduleFrom(expr, from)
}

func parseScheduleFrom(expr string, now time.Time) (time.Time, error) {
	expr = strings.TrimSpace(strings.ToLower(expr))
	parts := strings.Fields(expr)

	if len(parts) == 0 {
		return time.Time{}, fmt.Errorf("empty schedule expression")
	}

	switch parts[0] {
	case "every":
		return parseEvery(parts, now)
	case "daily":
		return parseDaily(parts, now)
	case "weekly":
		return parseWeekly(parts, now)
	default:
		return time.Time{}, fmt.Errorf("unsupported schedule type %q, use every/daily/weekly", parts[0])
	}
}

func parseEvery(parts []string, now time.Time) (time.Time, error) {
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid every expression, use: every 1h, every 30m")
	}

	d, err := time.ParseDuration(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid duration %q: %w", parts[1], err)
	}

	if d < time.Minute {
		return time.Time{}, fmt.Errorf("minimum interval is 1m")
	}

	return now.Add(d), nil
}

func parseDaily(parts []string, now time.Time) (time.Time, error) {
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid daily expression, use: daily 09:00")
	}

	hour, minute, err := parseTime(parts[1])
	if err != nil {
		return time.Time{}, err
	}

	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}

	return next, nil
}

var dayMap = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

func parseWeekly(parts []string, now time.Time) (time.Time, error) {
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid weekly expression, use: weekly mon 09:00")
	}

	targetDay, ok := dayMap[parts[1]]
	if !ok {
		return time.Time{}, fmt.Errorf("invalid day %q, use: sun, mon, tue, wed, thu, fri, sat", parts[1])
	}

	hour, minute, err := parseTime(parts[2])
	if err != nil {
		return time.Time{}, err
	}

	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)

	// Advance to the target weekday
	daysUntil := int(targetDay) - int(now.Weekday())
	if daysUntil < 0 {
		daysUntil += 7
	}

	next = next.AddDate(0, 0, daysUntil)

	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}

	return next, nil
}

func parseTime(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time %q, use HH:MM", s)
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}

	return hour, minute, nil
}
