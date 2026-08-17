package bot

import (
	"bytes"
	"image/png"
	"os"
	"testing"
	"time"

	"time-table-bot/internal/nlu"
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

func TestLooksLikeSchedulePlanRequest(t *testing.T) {
	for _, text := range []string{
		"сделай расписание на следующий месяц",
		"заполни график по будням с 10 до 17",
		"сделай как в этом месяце",
	} {
		if !looksLikeSchedulePlanRequest(text) {
			t.Errorf("looksLikeSchedulePlanRequest(%q) = false", text)
		}
	}
	if looksLikeSchedulePlanRequest("покажи расписание на неделю") {
		t.Fatal("display request must not create a schedule plan")
	}
}

func TestMaterializeSchedulePlanWeekdaysAndExtraSaturdays(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, loc)
	intent := nlu.AdminSchedulePlanIntent{
		IsSchedulePlan: true, TargetMonth: "2026-09", Confidence: 0.98,
		Rules: []nlu.AdminSchedulePlanRule{{Weekdays: []int{1, 2, 3, 4, 5}, Start: "10:00", End: "17:00"}},
		ExtraDays: []nlu.AdminSchedulePlanDay{
			{Date: "2026-09-05", Weekday: 6},
			{Date: "2026-09-19", Weekday: 6},
		},
	}
	plan, err := (&Bot{}).materializeSchedulePlan(t.Context(), UserRecord{Language: LangRU}, intent, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Days) != 24 || len(plan.Rules) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	for _, day := range plan.Days {
		if day.Extra && (day.Start != "10:00" || day.End != "17:00") {
			t.Fatalf("extra day did not inherit hours: %#v", day)
		}
	}
	image, err := renderSchedulePlanMonthImage(LangRU, plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 1120 || decoded.Bounds().Dy() != 600 {
		t.Fatalf("image bounds = %v", decoded.Bounds())
	}
	if output := os.Getenv("SCHEDULE_PLAN_IMAGE_OUTPUT"); output != "" {
		if err := os.WriteFile(output, image, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMaterializeSchedulePlanRejectsWrongClaimedWeekday(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	_, err := (&Bot{}).materializeSchedulePlan(t.Context(), UserRecord{Language: LangRU}, nlu.AdminSchedulePlanIntent{
		TargetMonth: "2026-09",
		Rules:       []nlu.AdminSchedulePlanRule{{Weekdays: []int{1, 2, 3, 4, 5}, Start: "10:00", End: "17:00"}},
		ExtraDays:   []nlu.AdminSchedulePlanDay{{Date: "2026-09-14", Weekday: 6}},
	}, time.Date(2026, 8, 17, 10, 0, 0, 0, loc))
	if err == nil {
		t.Fatal("expected weekday/date mismatch")
	}
}

func TestInferSchedulePlanRulesFromCurrentMonth(t *testing.T) {
	loc := time.Local
	slots := []ScheduleGridSlot{
		{StartAt: time.Date(2026, 8, 3, 10, 0, 0, 0, loc), EndAt: time.Date(2026, 8, 3, 10, 30, 0, 0, loc)},
		{StartAt: time.Date(2026, 8, 3, 10, 30, 0, 0, loc), EndAt: time.Date(2026, 8, 3, 11, 0, 0, 0, loc)},
		{StartAt: time.Date(2026, 8, 10, 10, 0, 0, 0, loc), EndAt: time.Date(2026, 8, 10, 17, 0, 0, 0, loc)},
		{StartAt: time.Date(2026, 8, 17, 10, 0, 0, 0, loc), EndAt: time.Date(2026, 8, 17, 17, 0, 0, 0, loc)},
	}
	rules, duration := inferSchedulePlanRules(slots)
	rule, ok := rules[time.Monday]
	if !ok || rule.Start != "10:00" || rule.End != "17:00" || duration != 30 {
		t.Fatalf("rules=%#v duration=%d", rules, duration)
	}
}

func TestParseAdminNaturalScheduleIgnoresUnrelatedText(t *testing.T) {
	if _, ok := parseAdminNaturalScheduleWeek("поменяй рабочее расписание", time.Now()); ok {
		t.Fatal("schedule settings text must not open the week image")
	}
}
