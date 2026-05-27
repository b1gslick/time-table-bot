package bot

import (
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
