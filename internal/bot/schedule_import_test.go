package bot

import (
	"strings"
	"testing"
	"time"
)

func TestLooksLikeScheduleImport(t *testing.T) {
	text := "Авг. 2026 г.\nНеделя 34\nпн 17\n9:30 Анастасия\nэлектро\n10:40 Катя\nвоск"
	if !looksLikeScheduleImport(text) {
		t.Fatal("weekly planner OCR was not detected")
	}
	if looksLikeScheduleImport("запиши Лизу завтра в 10:00") {
		t.Fatal("single booking was detected as schedule import")
	}
}

func TestFormatScheduleImportPreviewShowsSkippedReason(t *testing.T) {
	items := []evaluatedScheduleImport{
		{
			Draft: ScheduleImportDraft{Client: "Лиза", ContactType: "telegram", Contact: "liza", ServiceIndexes: []int{1}},
			Start: time.Date(2026, 8, 17, 9, 30, 0, 0, time.Local), ServiceText: "Электро", Ready: true,
		},
		{
			Draft: ScheduleImportDraft{Client: "Катя"}, Start: time.Date(2026, 8, 17, 10, 40, 0, 0, time.Local),
			Issue: "нет алиаса клиента",
		},
	}
	got := formatScheduleImportPreview(LangRU, items)
	for _, want := range []string{"+ 17.08 09:30 - Лиза (@liza) - Электро", "! 17.08 10:40 - Катя: нет алиаса клиента"} {
		if !strings.Contains(got, want) {
			t.Fatalf("preview %q does not contain %q", got, want)
		}
	}
}

func TestScheduleImportKeyboardRequiresReadyEntry(t *testing.T) {
	withoutConfirm := scheduleImportKeyboard(LangRU, false)
	if got := withoutConfirm.InlineKeyboard[0][0].CallbackData; got != "scheduleimport:retry" {
		t.Fatalf("first callback = %q", got)
	}
	withConfirm := scheduleImportKeyboard(LangRU, true)
	if got := withConfirm.InlineKeyboard[0][0].CallbackData; got != "scheduleimport:yes" {
		t.Fatalf("first callback = %q", got)
	}
}

func TestFormatScheduleImportPreviewMarksTemporaryClientName(t *testing.T) {
	items := []evaluatedScheduleImport{{
		Draft: ScheduleImportDraft{Client: "Анастасия Балтаджи", ContactType: "name", Contact: "Анастасия Балтаджи"},
		Start: time.Date(2026, 8, 19, 16, 50, 0, 0, time.Local), ServiceText: "Подмышки", Ready: true,
	}}
	got := formatScheduleImportPreview(LangRU, items)
	if !strings.Contains(got, "+ 19.08 16:50 - Анастасия Балтаджи [без алиаса] - Подмышки") {
		t.Fatalf("preview = %q", got)
	}
}

func TestScheduleImportServicesCoverEveryRecognizedType(t *testing.T) {
	services := []ServiceView{
		{Category: "Электроэпиляция", Name: "1 час 45 €"},
		{Category: "Восковая депиляция", Name: "Бикини глубокое"},
	}
	if scheduleImportServicesCoverQuery([]string{"электро и воск"}, []int{1}, services) {
		t.Fatal("electro-only selection must not cover a wax request")
	}
	if !scheduleImportServicesCoverQuery([]string{"электро подмышки"}, []int{1}, services) {
		t.Fatal("body zone must be allowed as a note for electrolysis")
	}
	if scheduleImportServicesCoverQuery([]string{"BOCK"}, []int{2}, services) {
		t.Fatal("generic wax text must not select an arbitrary body zone")
	}
	if !scheduleImportServicesCoverQuery([]string{"BOCK бикини"}, []int{2}, services) {
		t.Fatal("OCR spelling BOCK with a body zone must match the wax category")
	}
}

func TestScheduleImportRejectsAmbiguousTariff(t *testing.T) {
	services := []ServiceView{
		{Category: "Электроэпиляция", Name: "До 30 мин", DurationMin: 30},
		{Category: "Электроэпиляция", Name: "1 час", DurationMin: 60},
	}
	if scheduleImportServiceSelectionUnambiguous([]string{"электро"}, 0, []int{1}, services) {
		t.Fatal("generic category must not select the first tariff")
	}
	if !scheduleImportServiceSelectionUnambiguous([]string{"электро 1 час"}, 60, []int{2}, services) {
		t.Fatal("explicit duration must select the matching tariff")
	}
}
