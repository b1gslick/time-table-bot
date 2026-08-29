package bot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
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
	scheduleKeyboard := scheduleMenuKeyboard(LangRU)
	if got := scheduleKeyboard.Keyboard[1][0].Text; got != "Изменить расписание" {
		t.Fatalf("schedule change button = %q", got)
	}
	changeKeyboard := scheduleChangeKeyboard(LangRU)
	if got := changeKeyboard.Keyboard[0][0].Text; got != "Сгенерировать месяц" {
		t.Fatalf("generate month button = %q", got)
	}
	monthsKeyboard := scheduleMonthsKeyboard(LangRU, []ScheduleMonth{{Month: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}})
	if got := monthsKeyboard.Keyboard[0][0].Text; got != "2026-08" {
		t.Fatalf("schedule month button = %q", got)
	}
	daysKeyboard := scheduleDaysKeyboard(LangRU, []ScheduleDay{{Date: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)}})
	if got := daysKeyboard.Keyboard[0][0].Text; got != "2026-08-15" {
		t.Fatalf("schedule day button = %q", got)
	}
	weekdaysKeyboard := scheduleWeekdaysKeyboard(LangRU, []ScheduleWeekday{{Weekday: time.Thursday}})
	if got := weekdaysKeyboard.Keyboard[0][0].Text; got != "чт" {
		t.Fatalf("schedule weekday button = %q", got)
	}
	if got := menuButtonAction(LangRU, "Изменить расписание"); got != "action_schedule_change" {
		t.Fatalf("schedule change action = %q", got)
	}
	if got := menuButtonAction(LangEN, "Изменить один день"); got != "action_generate_day" {
		t.Fatalf("cross-language generate day action = %q", got)
	}
	servicesKeyboard := servicesMenuKeyboard(LangRU)
	if got := servicesKeyboard.Keyboard[0][1].Text; got != "Изменить услугу" {
		t.Fatalf("targeted service edit button = %q", got)
	}
	if got := servicesKeyboard.Keyboard[2][1].Text; got != "Заменить весь список" {
		t.Fatalf("service replacement button = %q", got)
	}
	if got := menuButtonAction(LangRU, "Заменить весь список"); got != "action_service_replace" {
		t.Fatalf("service replacement action = %q", got)
	}
	for _, row := range servicesKeyboard.Keyboard {
		for _, button := range row {
			if button.Text == "Описание услуг" {
				t.Fatalf("legacy services description is still in menu: %#v", servicesKeyboard.Keyboard)
			}
		}
	}
	if got := menuButtonAction(LangRU, "Описание услуг"); got != "" {
		t.Fatalf("legacy services description action = %q", got)
	}
	if mode, ok := parseGenerateMode(LangRU, "4"); !ok || mode != "weekday" {
		t.Fatalf("parseGenerateMode = %q, %v; want weekday, true", mode, ok)
	}
}

func TestStartKeyboardsDoNotExposeCommands(t *testing.T) {
	for _, role := range []Role{RoleUser, RoleAdmin, RoleSuperAdmin} {
		keyboard := keyboardForRole(role, LangRU)
		for _, row := range keyboard.Keyboard {
			for _, button := range row {
				if strings.HasPrefix(button.Text, "/") {
					t.Fatalf("role %s start button exposes command %q", role, button.Text)
				}
			}
		}
	}
	clientKeyboard := keyboardForRole(RoleUser, LangRU)
	if len(clientKeyboard.Keyboard) != 3 || clientKeyboard.Keyboard[0][0].Text != "Начать запись" || clientKeyboard.Keyboard[1][0].Text != "Календарь" || clientKeyboard.Keyboard[2][0].Text != "Мои записи" {
		t.Fatalf("client keyboard = %#v", clientKeyboard.Keyboard)
	}
}

func TestFormatStartServicesIsCompactAndCommandFree(t *testing.T) {
	services := []ServiceView{
		{Category: "Эпиляция", Subcategory: "Ноги", Name: "Голени", DurationMin: 45, Description: "Удаление волос", PriceText: "35 EUR"},
		{Category: "Эпиляция", Name: "Полный комплекс", DurationMin: 90, AdminName: "master"},
	}
	got := formatStartServices(LangRU, services, 1)
	for _, want := range []string{"Доступные услуги:", "• Эпиляция / Ноги / Голени · 35 € · 45 мин.\n  Удаление волос", "И еще услуг: 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("start services = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, "/schedule") || strings.Contains(got, "/book") {
		t.Fatalf("start services exposes commands: %q", got)
	}
}

func TestFormatClientServicesShowsPriceAndDurationInline(t *testing.T) {
	services := []ServiceView{{Name: "Только тело", DurationMin: 60, PriceText: "70 евро", Description: "Эндосфера тела"}}
	got := formatServicesList(LangRU, services, false)
	if !strings.Contains(got, "1. Только тело · 70 € · 60 мин.\n   Эндосфера тела") {
		t.Fatalf("client services = %q", got)
	}
}

func TestViewAdminKeyboardCallback(t *testing.T) {
	kb := viewAdminKeyboard(LangRU, []AdminView{{Username: "master", Role: RoleAdmin}})
	if kb == nil || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("keyboard = %#v, want one inline button", kb)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "viewadmin:master" {
		t.Fatalf("callback data = %q, want viewadmin:master", got)
	}
	text, ok := callbackText(kb.InlineKeyboard[0][0].CallbackData)
	if !ok || text != "master" {
		t.Fatalf("callbackText = %q, %v; want master, true", text, ok)
	}
}

func TestCancelBookingKeyboardCallback(t *testing.T) {
	start := time.Date(2026, 6, 3, 10, 0, 0, 0, time.Local)
	kb := cancelBookingKeyboard(LangRU, []BookingView{{
		ID:           42,
		Username:     "client",
		StartAt:      start,
		ServiceNames: []string{"Маникюр"},
	}})
	if kb == nil || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("keyboard = %#v, want one inline button", kb)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "cancel:42" {
		t.Fatalf("callback data = %q, want cancel:42", got)
	}
	if got := kb.InlineKeyboard[0][0].Text; !strings.Contains(got, "@client") || !strings.Contains(got, "Маникюр") {
		t.Fatalf("button text = %q, want client and service", got)
	}
}

func TestMyBookingInlineKeyboards(t *testing.T) {
	start := time.Date(2026, 6, 3, 10, 0, 0, 0, time.Local)
	items := []BookingView{{ID: 42, AdminName: "master", StartAt: start, ServiceNames: []string{"Маникюр"}}}

	actions := myActionsKeyboard(LangRU)
	if actions == nil || len(actions.InlineKeyboard) != 3 {
		t.Fatalf("actions keyboard = %#v, want 3 rows", actions)
	}
	if got := actions.InlineKeyboard[1][0].CallbackData; got != "mycancel:list" {
		t.Fatalf("cancel action callback = %q, want mycancel:list", got)
	}
	if got := actions.InlineKeyboard[1][1].CallbackData; got != "mymove:list" {
		t.Fatalf("move action callback = %q, want mymove:list", got)
	}

	cancel := myBookingActionKeyboard(LangRU, items, "mycancel")
	if got := cancel.InlineKeyboard[0][0].CallbackData; got != "mycancel:42" {
		t.Fatalf("cancel booking callback = %q, want mycancel:42", got)
	}
	move := myBookingActionKeyboard(LangRU, items, "mymove")
	if got := move.InlineKeyboard[0][0].CallbackData; got != "mymove:42" {
		t.Fatalf("move booking callback = %q, want mymove:42", got)
	}
	slots := moveSlotKeyboard(LangRU, 42, []AvailabilitySlot{{StartAt: start.Add(time.Hour), DurationMin: 30}})
	if got := slots.InlineKeyboard[0][0].CallbackData; got != "moveslot:42:1" {
		t.Fatalf("move slot callback = %q, want moveslot:42:1", got)
	}
}

func TestBookingConfirmationKeyboard(t *testing.T) {
	kb := bookingConfirmationKeyboard(LangRU)
	if kb == nil || len(kb.InlineKeyboard) != 2 {
		t.Fatalf("keyboard = %#v, want two rows", kb)
	}
	want := []string{"bookconfirm:yes", "bookconfirm:no", "bookconfirm:edit", "bookconfirm:other"}
	var got []string
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			got = append(got, button.CallbackData)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("callbacks = %#v, want %#v", got, want)
	}
	if got := kb.InlineKeyboard[1][0].Text; got != "Изменить" {
		t.Fatalf("edit text = %q", got)
	}
	if got := kb.InlineKeyboard[1][1].Text; got != "Найти другое" {
		t.Fatalf("find another text = %q", got)
	}
}

func TestBookingEditKeyboardIncludesClientOnlyForAdmin(t *testing.T) {
	client := bookingEditKeyboard(LangRU, false)
	if client == nil || len(client.InlineKeyboard) != 1 || len(client.InlineKeyboard[0]) != 2 {
		t.Fatalf("client edit keyboard = %#v", client)
	}
	admin := bookingEditKeyboard(LangRU, true)
	if admin == nil || len(admin.InlineKeyboard) != 2 {
		t.Fatalf("admin edit keyboard = %#v", admin)
	}
	if got := admin.InlineKeyboard[1][0].CallbackData; got != "bookedit:client" {
		t.Fatalf("client edit callback = %q", got)
	}
}

func TestNormalizeBookingConfirmationChoice(t *testing.T) {
	tests := map[string]string{
		"Да":           "yes",
		"нет":          "no",
		"Найти другое": "other",
		"другое время": "other",
		"Find another": "other",
		"Изменить":     "edit",
		"Услугу":       "service",
		"Дату и время": "time",
		"Клиента":      "client",
	}
	for input, want := range tests {
		if got := normalizeChoice(input); got != want {
			t.Fatalf("normalizeChoice(%q) = %q, want %q", input, got, want)
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

func TestResetSlotBrowserStateClearsStaleDay(t *testing.T) {
	slots := []AvailabilitySlot{
		{
			StartAt: time.Date(2026, 7, 3, 9, 30, 0, 0, time.Local),
			EndAt:   time.Date(2026, 7, 3, 10, 0, 0, 0, time.Local),
		},
		{
			StartAt: time.Date(2026, 7, 4, 9, 30, 0, 0, time.Local),
			EndAt:   time.Date(2026, 7, 4, 10, 0, 0, 0, time.Local),
		},
	}
	state := resetSlotBrowserState(ConversationState{
		Step:               conversationStepSlot,
		SlotDay:            "2026-07-01",
		SlotPeriod:         "day",
		PendingSlotIndex:   4,
		VisibleSlotIndexes: []int{10},
	})
	if state.PendingSlotIndex != 0 {
		t.Fatalf("PendingSlotIndex = %d, want 0", state.PendingSlotIndex)
	}

	text, _, next := renderSlotBrowser(LangRU, state, slots)
	if next.SlotDay != "2026-07-03" {
		t.Fatalf("SlotDay = %q, want first available day 2026-07-03; text: %s", next.SlotDay, text)
	}
	if next.SlotPeriod != "morning" {
		t.Fatalf("SlotPeriod = %q, want morning", next.SlotPeriod)
	}
	if len(next.VisibleSlotIndexes) != 1 || next.VisibleSlotIndexes[0] != 1 {
		t.Fatalf("VisibleSlotIndexes = %#v, want [1]", next.VisibleSlotIndexes)
	}
}

func TestSlotBrowserMovesByCalendarDayAndHasBack(t *testing.T) {
	slots := []AvailabilitySlot{
		{
			StartAt: time.Date(2026, 6, 26, 9, 30, 0, 0, time.Local),
			EndAt:   time.Date(2026, 6, 26, 10, 0, 0, 0, time.Local),
		},
		{
			StartAt: time.Date(2026, 7, 1, 9, 30, 0, 0, time.Local),
			EndAt:   time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local),
		},
	}
	nextDay := moveSlotDay(slots, "2026-06-26", "morning", "next")
	if nextDay != "2026-06-27" {
		t.Fatalf("next day = %q, want calendar day 2026-06-27", nextDay)
	}
	afterLastSlotDay := moveSlotDay(slots, "2026-07-01", "morning", "next")
	if afterLastSlotDay != "2026-07-02" {
		t.Fatalf("day after last slot day = %q, want calendar day 2026-07-02", afterLastSlotDay)
	}
	text, kb, next := renderSlotBrowser(LangRU, ConversationState{
		SlotDay:    nextDay,
		SlotPeriod: "morning",
	}, slots)
	if next.SlotDay != "2026-06-27" {
		t.Fatalf("rendered SlotDay = %q, want 2026-06-27; text: %s", next.SlotDay, text)
	}
	if len(next.VisibleSlotIndexes) != 0 {
		t.Fatalf("VisibleSlotIndexes = %#v, want empty day", next.VisibleSlotIndexes)
	}
	if !strings.Contains(text, "На выбранный период") {
		t.Fatalf("text = %q, want empty day message", text)
	}
	foundBack := false
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "back:slot" {
				foundBack = true
			}
		}
	}
	if !foundBack {
		t.Fatalf("keyboard = %#v, want back:slot button", kb)
	}
}

func TestAvailabilityDateKeyboardShowsAvailableDays(t *testing.T) {
	from := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	slots := []AvailabilitySlot{
		{StartAt: from.AddDate(0, 0, 1).Add(10 * time.Hour)},
		{StartAt: from.AddDate(0, 0, 1).Add(11 * time.Hour)},
		{StartAt: from.AddDate(0, 0, 3).Add(12 * time.Hour)},
		{StartAt: from.AddDate(0, 0, 8).Add(12 * time.Hour)},
	}
	kb := availabilityDateKeyboard(LangRU, slots, from, from.AddDate(0, 0, 7))
	if kb == nil || len(kb.InlineKeyboard) != 2 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("date keyboard = %#v", kb)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "slotdate:2026-06-23" {
		t.Fatalf("first date callback = %q", got)
	}
	if got := kb.InlineKeyboard[0][1].CallbackData; got != "slotdate:2026-06-25" {
		t.Fatalf("second date callback = %q", got)
	}
}

func TestScheduleGridForAvailabilityMarksUnavailableTimeBusy(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	grid := []ScheduleGridSlot{
		{StartAt: start, EndAt: start.Add(30 * time.Minute), Status: "open", Capacity: 1, Available: 1},
		{StartAt: start.Add(30 * time.Minute), EndAt: start.Add(time.Hour), Status: "open", Capacity: 1, Available: 1},
		{AdminName: "other", StartAt: start, EndAt: start.Add(30 * time.Minute), Status: "open", Capacity: 1, Available: 1},
	}
	availability := []AvailabilitySlot{{
		AdminName: "master", StartAt: start, EndAt: start.Add(30 * time.Minute),
	}}
	got := scheduleGridForAvailability(grid, availability, dateOnly(start), dateOnly(start).AddDate(0, 0, 7))
	if len(got) != 2 {
		t.Fatalf("grid len = %d, want only selected master", len(got))
	}
	if got[0].Available != 1 || got[0].Booked != 0 {
		t.Fatalf("available slot = %#v", got[0])
	}
	if got[1].Available != 0 || got[1].Booked != 1 {
		t.Fatalf("unavailable slot = %#v", got[1])
	}
}

func TestSlotBrowserPaginatesLargeDay(t *testing.T) {
	start := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	slots := make([]AvailabilitySlot, 0, 20)
	for i := 0; i < 20; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		slots = append(slots, AvailabilitySlot{StartAt: slotStart, EndAt: slotStart.Add(30 * time.Minute)})
	}
	text, kb, first := renderSlotBrowser(LangRU, ConversationState{SlotDay: "2026-08-26", SlotPeriod: "all"}, slots)
	if len(first.VisibleSlotIndexes) != 9 || first.VisibleSlotIndexes[0] != 1 || first.VisibleSlotIndexes[8] != 9 {
		t.Fatalf("first page indexes = %#v", first.VisibleSlotIndexes)
	}
	if !strings.Contains(text, "Страница 1/3") {
		t.Fatalf("first page text = %q", text)
	}
	foundNext := false
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "slotpage:next" {
				foundNext = true
			}
		}
	}
	if !foundNext {
		t.Fatalf("first page keyboard has no next button: %#v", kb)
	}
	text, _, last := renderSlotBrowser(LangRU, ConversationState{SlotDay: "2026-08-26", SlotPeriod: "all", SlotPage: 2}, slots)
	if len(last.VisibleSlotIndexes) != 2 || last.VisibleSlotIndexes[0] != 19 || last.VisibleSlotIndexes[1] != 20 {
		t.Fatalf("last page indexes = %#v", last.VisibleSlotIndexes)
	}
	if !strings.Contains(text, "Страница 3/3") {
		t.Fatalf("last page text = %q", text)
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

func TestRenderCalendarMonthImageProducesPNG(t *testing.T) {
	month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rendered, err := renderCalendarMonthImage(LangRU, month, []CalendarDay{
		{Date: month, OpenSlots: 3, TotalSlots: 4},
		{Date: month.AddDate(0, 0, 1), Booked: 2, TotalSlots: 2},
	}, true)
	if err != nil {
		t.Fatalf("renderCalendarMonthImage error: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(rendered))
	if err != nil {
		t.Fatalf("DecodeConfig error: %v", err)
	}
	if cfg.Width != 1120 || cfg.Height < 700 {
		t.Fatalf("image size = %dx%d, want 1120 and at least 700", cfg.Width, cfg.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(rendered))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !imageContainsColor(decoded, scheduleClientBusy) {
		t.Fatal("private monthly calendar does not contain gray busy time")
	}
}

func TestRenderCalendarMonthImageGroupsByAdmin(t *testing.T) {
	month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rendered, err := renderCalendarMonthImage(LangRU, month, []CalendarDay{
		{AdminName: "master", Date: month, OpenSlots: 2, TotalSlots: 2},
		{AdminName: "second", Date: month, Booked: 1, TotalSlots: 1},
	}, true)
	if err != nil {
		t.Fatalf("renderCalendarMonthImage error: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(rendered))
	if err != nil {
		t.Fatalf("DecodeConfig error: %v", err)
	}
	if cfg.Height < 1400 {
		t.Fatalf("grouped calendar height = %d, want two panels", cfg.Height)
	}
}

func TestRenderScheduleWeekImageProducesPNG(t *testing.T) {
	week := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	image, err := renderScheduleWeekImage(LangRU, week, []ScheduleGridSlot{
		{
			StartAt:   week.Add(10 * time.Hour),
			EndAt:     week.Add(10*time.Hour + 30*time.Minute),
			Status:    "open",
			Capacity:  1,
			Available: 1,
		},
		{
			StartAt:  week.AddDate(0, 0, 1).Add(11 * time.Hour),
			EndAt:    week.AddDate(0, 0, 1).Add(11*time.Hour + 30*time.Minute),
			Status:   "open",
			Capacity: 1,
			Booked:   1,
		},
		{
			StartAt:  week.AddDate(0, 0, 2).Add(12 * time.Hour),
			EndAt:    week.AddDate(0, 0, 2).Add(12*time.Hour + 30*time.Minute),
			Status:   "closed",
			Capacity: 1,
		},
	}, []BookingView{{
		Username:     "client",
		ServiceNames: []string{"Эпиляция"},
		StartAt:      week.AddDate(0, 0, 1).Add(11 * time.Hour),
		EndAt:        week.AddDate(0, 0, 1).Add(12*time.Hour + 30*time.Minute),
	}})
	if err != nil {
		t.Fatalf("renderScheduleWeekImage error: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("DecodeConfig error: %v", err)
	}
	if cfg.Width != scheduleImageWidth || cfg.Height < 1000 {
		t.Fatalf("image size = %dx%d, want width %d and tall image", cfg.Width, cfg.Height, scheduleImageWidth)
	}
}

func TestScheduleBookingLinesKeepContactVisible(t *testing.T) {
	start := time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC)
	booking := BookingView{
		Alias: "анастасия балтаджи", Username: "+357991234567890",
		ServiceNames: []string{"Эпиляция"}, StartAt: start, EndAt: start.Add(30 * time.Minute),
	}

	short := scheduleBookingLines(booking, 42)
	wantShort := []string{"анастасия балтаджи", "Эпиляция", "+357991234567890"}
	if len(short) != len(wantShort) {
		t.Fatalf("short booking lines = %#v", short)
	}
	for i, text := range wantShort {
		if short[i].Text != text {
			t.Fatalf("short booking line %d = %q, want %q", i, short[i].Text, text)
		}
	}
	tiny := scheduleBookingLines(booking, 24)
	if len(tiny) != 1 || tiny[0].Text != "анастасия балтаджи" {
		t.Fatalf("tiny booking lines = %#v, alias must have priority", tiny)
	}
	tall := scheduleBookingLines(booking, 90)
	want := []string{"анастасия балтаджи", "Эпиляция", "+357991234567890"}
	if len(tall) != len(want) {
		t.Fatalf("tall booking lines = %#v", tall)
	}
	for i, text := range want {
		if tall[i].Text != text {
			t.Fatalf("tall booking line %d = %q, want %q", i, tall[i].Text, text)
		}
	}
}

func TestWrapScheduleServiceKeepsFullName(t *testing.T) {
	fonts, err := newScheduleFontSet()
	if err != nil {
		t.Fatalf("newScheduleFontSet: %v", err)
	}
	defer fonts.close()

	service := "Эндосфера — только тело и зона декольте"
	lines := wrapFontText(service, 150, fonts.compact, 2)
	if len(lines) != 2 || strings.Join(lines, " ") != service {
		t.Fatalf("wrapped service = %#v, want full service name", lines)
	}
	for _, line := range lines {
		if font.MeasureString(fonts.compact, line).Ceil() > 150 {
			t.Fatalf("wrapped line %q is too wide", line)
		}
	}
}

func TestLargeScheduleBookingContentIsLowered(t *testing.T) {
	if got := scheduleBookingContentTop(160, 21, 4); got != 38 {
		t.Fatalf("large booking content top = %d, want 38", got)
	}
	if got := scheduleBookingContentTop(60, 18, 3); got != 2 {
		t.Fatalf("compact booking content top = %d, want 2", got)
	}
}

func TestPrivateScheduleImageDropsBookingDetailsAndUsesGray(t *testing.T) {
	week := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	slots := []ScheduleGridSlot{{
		AdminName: "master", StartAt: week.Add(10 * time.Hour), EndAt: week.Add(10*time.Hour + 30*time.Minute),
		Status: "open", Capacity: 1, Booked: 1, Available: 0,
	}}
	bookings := []BookingView{{
		AdminName: "master", Username: "private-client", ServiceNames: []string{"Private service"},
		StartAt: week.Add(10 * time.Hour), EndAt: week.Add(10*time.Hour + 30*time.Minute),
	}}
	groups := scheduleGroupsForAudience(slots, bookings, true)
	if len(groups) != 1 || len(groups[0].Bookings) != 0 {
		t.Fatalf("private groups contain booking details: %#v", groups)
	}
	rendered, err := renderScheduleWeekImageForAudience(LangRU, week, slots, bookings, true)
	if err != nil {
		t.Fatalf("render private schedule: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(rendered))
	if err != nil {
		t.Fatalf("decode private schedule: %v", err)
	}
	if !imageContainsColor(decoded, scheduleClientBusy) {
		t.Fatal("private schedule does not contain gray busy slots")
	}
}

func imageContainsColor(decoded interface {
	Bounds() image.Rectangle
	At(x, y int) color.Color
}, target color.RGBA) bool {
	want := color.RGBAModel.Convert(target).(color.RGBA)
	for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
		for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
			if color.RGBAModel.Convert(decoded.At(x, y)).(color.RGBA) == want {
				return true
			}
		}
	}
	return false
}

func TestWeekNavigationKeyboardUsesAdjacentWeeks(t *testing.T) {
	start := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	kb := weekNavigationKeyboard(LangRU, start)
	if kb == nil || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 3 {
		t.Fatalf("keyboard = %#v, want one row with three buttons", kb)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "week:2026-06-15" {
		t.Fatalf("prev callback = %q, want week:2026-06-15", got)
	}
	if got := kb.InlineKeyboard[0][2].CallbackData; got != "week:2026-06-29" {
		t.Fatalf("next callback = %q, want week:2026-06-29", got)
	}
}

func TestCalendarNavigationKeyboardUsesAdjacentMonths(t *testing.T) {
	month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	kb := calendarNavigationKeyboard(LangRU, month)
	if kb == nil || len(kb.InlineKeyboard) != 2 || len(kb.InlineKeyboard[0]) != 3 {
		t.Fatalf("keyboard = %#v, want navigation and week rows", kb)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "monthcal:2026-05" {
		t.Fatalf("prev callback = %q", got)
	}
	if got := kb.InlineKeyboard[0][2].CallbackData; got != "monthcal:2026-07" {
		t.Fatalf("next callback = %q", got)
	}
}

func TestWeekStartUsesMonday(t *testing.T) {
	sunday := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	got := weekStart(sunday)
	want := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("weekStart = %s, want %s", got, want)
	}
}

func TestAdminBookingsRangeParsesRelativeDays(t *testing.T) {
	from, to, day, daily, err := adminBookingsRange([]string{"/bookings", "завтра"})
	if err != nil {
		t.Fatalf("adminBookingsRange error: %v", err)
	}
	wantDay := dateOnly(time.Now()).AddDate(0, 0, 1)
	if !daily || !day.Equal(wantDay) || !from.Equal(wantDay) || !to.Equal(wantDay.AddDate(0, 0, 1)) {
		t.Fatalf("range = from %s to %s day %s daily %v, want tomorrow day range", from, to, day, daily)
	}
}

func TestFormatAdminBookingsIncludesTimeClientAndService(t *testing.T) {
	start := time.Date(2026, 6, 3, 10, 0, 0, 0, time.Local)
	got := formatAdminBookings(LangRU, "Записи клиентов на 03.06.2026:\n", []BookingView{
		{Alias: "лиза", Username: "client", StartAt: start, EndAt: start.Add(time.Hour), ServiceNames: []string{"Маникюр"}},
	}, 30, true)
	if !strings.Contains(got, "2026-06-03 10:00 - лиза (@client) - Маникюр") {
		t.Fatalf("formatted bookings = %q", got)
	}
}
