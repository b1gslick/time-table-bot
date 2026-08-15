package bot

import (
	"bytes"
	"image/png"
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
	if mode, ok := parseGenerateMode(LangRU, "4"); !ok || mode != "weekday" {
		t.Fatalf("parseGenerateMode = %q, %v; want weekday, true", mode, ok)
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
		VisibleSlotIndexes: []int{10},
	})

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
	})
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
		{Username: "client", StartAt: start, EndAt: start.Add(time.Hour), ServiceNames: []string{"Маникюр"}},
	}, 30, true)
	if !strings.Contains(got, "2026-06-03 10:00 - @client - Маникюр") {
		t.Fatalf("formatted bookings = %q", got)
	}
}
