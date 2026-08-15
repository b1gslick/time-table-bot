package bot

import (
	"context"
	"regexp"
	"strings"
	"time"
)

var naturalScheduleDatePattern = regexp.MustCompile(`\b(?:\d{4}-\d{2}-\d{2}|\d{1,2}\.\d{1,2}(?:\.\d{2,4})?)\b`)

func (b *Bot) handleAdminNaturalSchedule(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if !isAdmin(user.Role) {
		return false, nil
	}
	start, ok := parseAdminNaturalScheduleWeek(text, time.Now())
	if !ok {
		return false, nil
	}
	return true, b.handleWeek(ctx, chatID, user, []string{"/week", start.Format("2006-01-02")})
}

func parseAdminNaturalScheduleWeek(text string, now time.Time) (time.Time, bool) {
	normalized := normalizeMatchText(text)
	if normalized == "" || !containsAnyPhrase(normalized, []string{"график", "расписан", "schedule", "timetable"}) {
		return time.Time{}, false
	}
	if !containsAnyPhrase(normalized, []string{
		"покаж", "пришл", "отправ", "дай", "за недел", "на недел", "текущ", "следующ", "прошл", "предыдущ",
		"show", "send", "this week", "next week", "last week", "current week",
	}) {
		return time.Time{}, false
	}

	now = dateOnly(now)
	if strings.Contains(normalized, "следующ") || strings.Contains(normalized, "next week") {
		return weekStart(now.AddDate(0, 0, 7)), true
	}
	if strings.Contains(normalized, "прошл") || strings.Contains(normalized, "предыдущ") || strings.Contains(normalized, "last week") {
		return weekStart(now.AddDate(0, 0, -7)), true
	}
	if rawDate := naturalScheduleDatePattern.FindString(text); rawDate != "" {
		if parsed, err := parseUserDate(rawDate, now); err == nil {
			return weekStart(parsed), true
		}
	}
	return weekStart(now), true
}

func containsAnyPhrase(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}
