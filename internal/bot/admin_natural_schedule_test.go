package bot

import (
	"testing"
	"time"
)

func TestParseAdminNaturalScheduleWeek(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, loc)
	tests := []struct {
		text string
		want time.Time
	}{
		{text: "покажи график за неделю", want: time.Date(2026, 8, 10, 0, 0, 0, 0, loc)},
		{text: "покажи расписание на следующую неделю", want: time.Date(2026, 8, 17, 0, 0, 0, 0, loc)},
		{text: "пришли график за прошлую неделю", want: time.Date(2026, 8, 3, 0, 0, 0, 0, loc)},
		{text: "покажи график за 25.08.2026", want: time.Date(2026, 8, 24, 0, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		got, ok := parseAdminNaturalScheduleWeek(tt.text, now)
		if !ok || !got.Equal(tt.want) {
			t.Errorf("parseAdminNaturalScheduleWeek(%q) = %s, %v; want %s, true", tt.text, got, ok, tt.want)
		}
	}
}

func TestParseAdminNaturalScheduleIgnoresUnrelatedText(t *testing.T) {
	if _, ok := parseAdminNaturalScheduleWeek("поменяй рабочее расписание", time.Now()); ok {
		t.Fatal("schedule settings text must not open the week image")
	}
}
