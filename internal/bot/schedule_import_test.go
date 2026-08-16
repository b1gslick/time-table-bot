package bot

import (
	"strings"
	"testing"
	"time"

	"time-table-bot/internal/nlu"
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

func TestFormatScheduleImportCurrentShowsOneItemAndProgress(t *testing.T) {
	item := evaluatedScheduleImport{
		Draft: ScheduleImportDraft{Client: "Лиза", ContactType: "telegram", Contact: "liza", ServiceIndexes: []int{1}},
		Start: time.Date(2026, 8, 17, 9, 30, 0, 0, time.Local), ServiceText: "Электро", Ready: true,
	}
	got := formatScheduleImportCurrent(LangRU, item, 1, 4, 1, 0)
	for _, want := range []string{"Проверка записи 2 из 4", "17.08.2026 09:30", "Лиза (@liza)", "Услуга: Электро", "Уже сохранено: 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("current item %q does not contain %q", got, want)
		}
	}
}

func TestScheduleImportKeyboardUsesCurrentIndex(t *testing.T) {
	withoutSave := scheduleImportItemKeyboard(LangRU, 3, false)
	if got := withoutSave.InlineKeyboard[0][0].CallbackData; got != "scheduleimport:edit:3" {
		t.Fatalf("first callback without save = %q", got)
	}
	withSave := scheduleImportItemKeyboard(LangRU, 3, true)
	if got := withSave.InlineKeyboard[0][0].CallbackData; got != "scheduleimport:save:3" {
		t.Fatalf("first callback with save = %q", got)
	}
	if got := withSave.InlineKeyboard[1][0].CallbackData; got != "scheduleimport:skip:3" {
		t.Fatalf("skip callback = %q", got)
	}
}

func TestFormatScheduleImportPreviewMarksTemporaryClientName(t *testing.T) {
	item := evaluatedScheduleImport{
		Draft: ScheduleImportDraft{Client: "Анастасия Балтаджи", ContactType: "name", Contact: "Анастасия Балтаджи"},
		Start: time.Date(2026, 8, 19, 16, 50, 0, 0, time.Local), ServiceText: "Подмышки", Ready: true,
	}
	got := formatScheduleImportCurrent(LangRU, item, 0, 1, 0, 0)
	if !strings.Contains(got, "Клиент: Анастасия Балтаджи [без алиаса]") {
		t.Fatalf("preview = %q", got)
	}
}

func TestFormatScheduleImportConflictShowsExistingBooking(t *testing.T) {
	conflict := BookingConflict{
		Username: "Анастасия Балтаджи", ServiceNames: []string{"Электроэпиляция — 1 час"},
		StartAt: time.Date(2026, 8, 17, 9, 30, 0, 0, time.Local),
		EndAt:   time.Date(2026, 8, 17, 10, 30, 0, 0, time.Local),
	}
	got := formatScheduleImportConflict(LangRU, conflict)
	for _, want := range []string{"17.08.2026 09:30–10:30", "Анастасия Балтаджи", "Электроэпиляция — 1 час"} {
		if !strings.Contains(got, want) {
			t.Fatalf("conflict %q does not contain %q", got, want)
		}
	}
}

func TestParseScheduleImportClientEditKeepsNameWithTelegram(t *testing.T) {
	client, contactType, contact, ok := parseScheduleImportClientEdit("Анастасия Балтаджи @hasti69", "")
	if !ok || client != "Анастасия Балтаджи" || contactType != "telegram" || contact != "hasti69" {
		t.Fatalf("client edit = %q %q %q %v", client, contactType, contact, ok)
	}
}

func TestResolveScheduleImportEditServices(t *testing.T) {
	services := []ServiceView{
		{Category: "Электроэпиляция", Name: "1 час", DurationMin: 60},
		{Category: "Восковая депиляция", Name: "Бикини", DurationMin: 30},
	}
	indexes := resolveScheduleImportEditServices("1, 2", services)
	if len(indexes) != 2 || indexes[0] != 1 || indexes[1] != 2 {
		t.Fatalf("indexes = %v, want [1 2]", indexes)
	}
	if got := scheduleImportDurationForIndexes(indexes, services); got != 90 {
		t.Fatalf("duration = %d, want 90", got)
	}
	choices := formatScheduleImportServiceChoices(LangRU, services)
	if !strings.Contains(choices, "1. Электроэпиляция / 1 час — 60 мин") || strings.Contains(choices, "/schedule") {
		t.Fatalf("service choices = %q", choices)
	}
}

func TestParseScheduleImportEditDateTime(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, loc)
	for input, want := range map[string]string{
		"19.08.2026 16:50": "2026-08-19T16:50:00+03:00",
		"завтра в 10:30":   "2026-08-17T10:30:00+03:00",
	} {
		got, err := parseScheduleImportEditDateTime(input, now)
		if err != nil || got.Format(time.RFC3339) != want {
			t.Fatalf("parse %q = %s, %v; want %s", input, got, err, want)
		}
	}
}

func TestParseScheduleImportEditableRecord(t *testing.T) {
	dateTime, client, service, ok := parseScheduleImportEditableRecord("19.08.2026 16:50 | Анастасия @hasti69 | Электроэпиляция — Подмышки")
	if !ok || dateTime != "19.08.2026 16:50" || client != "Анастасия @hasti69" || service != "Электроэпиляция — Подмышки" {
		t.Fatalf("editable record = %q, %q, %q, %v", dateTime, client, service, ok)
	}
	dateTime, client, service, ok = parseScheduleImportEditableRecord("Дата и время: 20.08.2026 10:00\nКлиент: Лиза\nУслуга: Электроэпиляция")
	if !ok || dateTime != "20.08.2026 10:00" || client != "Лиза" || service != "Электроэпиляция" {
		t.Fatalf("labeled editable record = %q, %q, %q, %v", dateTime, client, service, ok)
	}
}

func TestParseScheduleImportRecordPatchAcceptsMinimalCorrections(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.Local)
	patch, ok := parseScheduleImportRecordPatch("20.08.2026 10:00", now)
	if !ok || !patch.HasDateTime || patch.HasClient || patch.HasService || patch.DateTime != "20.08.2026 10:00" {
		t.Fatalf("date-only patch = %#v, %v", patch, ok)
	}
	patch, ok = parseScheduleImportRecordPatch("дата 20.08.2026 10:00\nуслуга электроэпиляция 1 час", now)
	if !ok || !patch.HasDateTime || !patch.HasService || patch.HasClient || patch.Service != "электроэпиляция 1 час" {
		t.Fatalf("multi-field patch = %#v, %v", patch, ok)
	}
}

func TestParseScheduleImportRecordPatchRejectsDateThatConsumesOtherCorrections(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.Local)
	patch, ok := parseScheduleImportRecordPatch("Дату на 18, услуги электроэпиляция на полтора часа и восковая эпиляция бикини", now)
	if ok || patch.HasDateTime || patch.HasService {
		t.Fatalf("ambiguous local patch must fall back to Qwen: %#v, %v", patch, ok)
	}
}

func TestScheduleImportServiceChangesDoNotAddExtraService(t *testing.T) {
	services := []ServiceView{
		{Category: "Электроэпиляция", Name: "До 30 мин 25€", DurationMin: 30},
		{Category: "Электроэпиляция", Name: "30 мин 25€", DurationMin: 30},
		{Category: "Электроэпиляция", Name: "1 час 45 €", DurationMin: 60},
		{Category: "Электроэпиляция", Name: "2 часа 90€", DurationMin: 120},
		{Category: "Восковая депиляция", Name: "Бикини классика", DurationMin: 15},
	}
	changes := []nlu.AdminScheduleEditService{
		{ServiceIndexes: []int{1, 3}, ServiceQueries: []string{"электроэпиляция на полтора часа"}, DurationMin: 90},
		{ServiceIndexes: []int{5}, ServiceQueries: []string{"восковая эпиляция бикини"}, DurationMin: 15},
	}
	var indexes []int
	for _, change := range changes {
		resolved := append([]int(nil), change.ServiceIndexes...)
		if !scheduleImportIndexesMatchDuration(resolved, change.DurationMin, services) {
			resolved = resolveNaturalBookingServices(nlu.BookingIntent{
				ServiceIndexes: change.ServiceIndexes, ServiceQueries: change.ServiceQueries, DurationMin: change.DurationMin,
			}, strings.Join(change.ServiceQueries, ", "), services)
		}
		for _, index := range resolved {
			if !intInSlice(indexes, index) {
				indexes = append(indexes, index)
			}
		}
	}
	if len(indexes) != 3 || indexes[0] != 1 || indexes[1] != 3 || indexes[2] != 5 {
		t.Fatalf("resolved service indexes = %v, want [1 3 5]", indexes)
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

func TestScheduleImportDoesNotDisplayArbitraryWaxService(t *testing.T) {
	if got := scheduleImportUnresolvedServiceText(LangRU, []string{"BOCK"}); got != "воск — зона не указана" {
		t.Fatalf("unresolved service text = %q", got)
	}
}
