package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronMatcher struct {
	minute [60]bool
	hour   [24]bool
	dom    [32]bool
	month  [13]bool
	dow    [7]bool
}

func parseCron(expr string) (cronMatcher, error) {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 5 {
		return cronMatcher{}, fmt.Errorf("cron expression must have 5 fields")
	}
	m := cronMatcher{}
	if err := parseField(parts[0], 0, 59, m.minute[:]); err != nil {
		return cronMatcher{}, fmt.Errorf("minute: %w", err)
	}
	if err := parseField(parts[1], 0, 23, m.hour[:]); err != nil {
		return cronMatcher{}, fmt.Errorf("hour: %w", err)
	}
	if err := parseField(parts[2], 1, 31, m.dom[:]); err != nil {
		return cronMatcher{}, fmt.Errorf("day-of-month: %w", err)
	}
	if err := parseField(parts[3], 1, 12, m.month[:]); err != nil {
		return cronMatcher{}, fmt.Errorf("month: %w", err)
	}
	if err := parseField(parts[4], 0, 6, m.dow[:]); err != nil {
		if strings.Contains(parts[4], "7") {
			fixed := strings.ReplaceAll(parts[4], "7", "0")
			if err2 := parseField(fixed, 0, 6, m.dow[:]); err2 != nil {
				return cronMatcher{}, fmt.Errorf("day-of-week: %w", err)
			}
		} else {
			return cronMatcher{}, fmt.Errorf("day-of-week: %w", err)
		}
	}
	return m, nil
}

func nextCron(expr string, from time.Time, loc *time.Location) (time.Time, error) {
	matcher, err := parseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	candidate := from.In(loc).Add(time.Minute).Truncate(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if matcher.match(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching cron time found within 1 year")
}

func (m cronMatcher) match(t time.Time) bool {
	minute := t.Minute()
	hour := t.Hour()
	dom := t.Day()
	month := int(t.Month())
	dow := int(t.Weekday())
	if minute < 0 || minute >= len(m.minute) {
		return false
	}
	if hour < 0 || hour >= len(m.hour) {
		return false
	}
	if dom < 0 || dom >= len(m.dom) {
		return false
	}
	if month < 0 || month >= len(m.month) {
		return false
	}
	if dow < 0 || dow >= len(m.dow) {
		return false
	}
	return m.minute[minute] && m.hour[hour] && m.dom[dom] && m.month[month] && m.dow[dow]
}

func parseField(raw string, min, max int, out []bool) error {
	for i := range out {
		out[i] = false
	}
	if strings.TrimSpace(raw) == "*" {
		for i := min; i <= max; i++ {
			out[i] = true
		}
		return nil
	}

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step <= 0 {
				return fmt.Errorf("invalid step value %q", part)
			}
			for i := min; i <= max; i += step {
				out[i] = true
			}
			continue
		}
		if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			if len(r) != 2 {
				return fmt.Errorf("invalid range %q", part)
			}
			start, err1 := strconv.Atoi(r[0])
			end, err2 := strconv.Atoi(r[1])
			if err1 != nil || err2 != nil {
				return fmt.Errorf("invalid range %q", part)
			}
			if start < min || end > max || start > end {
				return fmt.Errorf("range out of bounds %q", part)
			}
			for i := start; i <= end; i++ {
				out[i] = true
			}
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value %q", part)
		}
		if v < min || v > max {
			return fmt.Errorf("value out of bounds %q", part)
		}
		out[v] = true
	}

	any := false
	for i := min; i <= max; i++ {
		if out[i] {
			any = true
			break
		}
	}
	if !any {
		return fmt.Errorf("no values selected")
	}
	return nil
}
