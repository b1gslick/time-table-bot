package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseWeekdays(raw string) ([]time.Weekday, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "")

	if strings.Contains(raw, "-") && !strings.Contains(raw, ",") {
		parts := strings.Split(raw, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("bad weekday range")
		}
		start, ok := parseWeekday(parts[0])
		if !ok {
			return nil, fmt.Errorf("bad weekday")
		}
		end, ok := parseWeekday(parts[1])
		if !ok {
			return nil, fmt.Errorf("bad weekday")
		}
		return weekdayRange(start, end), nil
	}

	var out []time.Weekday
	seen := map[time.Weekday]bool{}
	for _, part := range strings.Split(raw, ",") {
		day, ok := parseWeekday(part)
		if !ok {
			return nil, fmt.Errorf("bad weekday")
		}
		if !seen[day] {
			out = append(out, day)
			seen[day] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty weekdays")
	}
	return out, nil
}

func parseWeekday(raw string) (time.Weekday, bool) {
	switch raw {
	case "mon", "monday", "1", "пн", "понедельник", "понедельники":
		return time.Monday, true
	case "tue", "tuesday", "2", "вт", "вторник", "вторники":
		return time.Tuesday, true
	case "wed", "wednesday", "3", "ср", "среда", "среды":
		return time.Wednesday, true
	case "thu", "thursday", "4", "чт", "четверг", "четверги":
		return time.Thursday, true
	case "fri", "friday", "5", "пт", "пятница", "пятницы":
		return time.Friday, true
	case "sat", "saturday", "6", "сб", "суббота", "субботы":
		return time.Saturday, true
	case "sun", "sunday", "7", "вс", "воскресенье", "воскресенья":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func weekdayRange(start, end time.Weekday) []time.Weekday {
	var out []time.Weekday
	for day := start; ; day = (day + 1) % 7 {
		out = append(out, day)
		if day == end {
			return out
		}
	}
}

func parseDayRange(raw string) (time.Duration, time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad range")
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func parseClock(raw string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("bad clock")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("bad hour")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("bad minute")
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute, nil
}
