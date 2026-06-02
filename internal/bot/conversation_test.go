package bot

import (
	"strings"
	"testing"
	"time"
)

func TestParseLanguageChoice(t *testing.T) {
	tests := map[string]string{
		"Русский": LangRU,
		"ru":      LangRU,
		"English": LangEN,
		"en":      LangEN,
	}
	for input, want := range tests {
		got, ok := parseLanguageChoice(input)
		if !ok || got != want {
			t.Fatalf("parseLanguageChoice(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestAdminMenuButtonsAreLocalizedAndRecognized(t *testing.T) {
	ruKeyboard := keyboardForRole(RoleSuperAdmin, LangRU)
	if got := ruKeyboard.Keyboard[0][0].Text; got != "Календарь" {
		t.Fatalf("ru first button = %q", got)
	}
	enKeyboard := keyboardForRole(RoleSuperAdmin, LangEN)
	if got := enKeyboard.Keyboard[0][0].Text; got != "Calendar" {
		t.Fatalf("en first button = %q", got)
	}
	if got := menuButtonAction(LangRU, "Calendar"); got != "menu_calendar" {
		t.Fatalf("english label in ru mode action = %q", got)
	}
	if got := menuButtonAction(LangEN, "Календарь"); got != "menu_calendar" {
		t.Fatalf("russian label in en mode action = %q", got)
	}
}

func TestParseDateList(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.Local)
	got, err := parseDateList("2026-06-01, 03.06.2026, 04.06", now)
	if err != nil {
		t.Fatalf("parseDateList error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []time.Time{
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local),
		time.Date(2026, 6, 3, 0, 0, 0, 0, time.Local),
		time.Date(2026, 6, 4, 0, 0, 0, 0, time.Local),
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("date[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestParseDateListRejectsBadDate(t *testing.T) {
	if _, err := parseDateList("not-a-date", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := map[string]string{
		"+357 99 999999": "+35799999999",
		"(999) 12-34":    "9991234",
		"abc":            "",
		"1234":           "",
	}
	for input, want := range tests {
		if got := normalizePhone(input); got != want {
			t.Fatalf("normalizePhone(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFormatCalendar(t *testing.T) {
	month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := formatCalendar(LangRU, month, []CalendarDay{
		{Date: month, OpenSlots: 3, TotalSlots: 4},
		{Date: month.AddDate(0, 0, 1), Booked: 2, TotalSlots: 2},
	})
	if !strings.Contains(got, "Календарь 2026-06") {
		t.Fatalf("calendar title missing: %s", got)
	}
	if !strings.Contains(got, " 13 ") {
		t.Fatalf("open marker missing: %s", got)
	}
	if !strings.Contains(got, " 2x ") {
		t.Fatalf("busy marker missing: %s", got)
	}
	if !strings.Contains(got, "01: свободно 3") {
		t.Fatalf("day summary missing: %s", got)
	}
}

func TestFormatCalendarGroupsSuperAdminViewByAdmin(t *testing.T) {
	month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := formatCalendar(LangRU, month, []CalendarDay{
		{AdminName: "master", Date: month, OpenSlots: 2, TotalSlots: 2},
		{AdminName: "second", Date: month, Booked: 1, TotalSlots: 1},
	})
	if !strings.Contains(got, "@master\nКалендарь 2026-06") {
		t.Fatalf("master calendar section missing: %s", got)
	}
	if !strings.Contains(got, "@second\nКалендарь 2026-06") {
		t.Fatalf("second calendar section missing: %s", got)
	}
	if !strings.Contains(got, "01: свободно 2") {
		t.Fatalf("master day summary missing: %s", got)
	}
	if !strings.Contains(got, "01: свободно 0, записей 1") {
		t.Fatalf("second day summary missing: %s", got)
	}
}
