package bot

import (
	"context"
	"testing"
	"time"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/telegram"
)

func TestLooksLikeAdminBookingCandidate(t *testing.T) {
	services := []ServiceView{{Name: "Эпиляция", DurationMin: 90}}
	for _, text := range []string{
		"запиши @client на эпиляцию завтра в 18:00",
		"@client хочу эпиляцию завтра вечером",
		"хочу эпиляцию завтра вечером",
		"book client @client tomorrow",
	} {
		if !looksLikeAdminBookingCandidate(text, services) {
			t.Fatalf("text %q was not recognized as candidate", text)
		}
	}
}

func TestExplicitAdminBookingRequestRestartsActiveDraft(t *testing.T) {
	state := ConversationState{
		Step:         conversationStepCategory,
		BookingDraft: "admin",
		Username:     "old-client",
		FromDateTime: "2026-09-15T10:20:00+03:00",
	}
	if !isAdminBookingConversation(state) {
		t.Fatal("admin booking draft was not recognized as an active booking conversation")
	}
	if !isExplicitAdminBookingRequest("Запиши Николь воск ноги до колен, руки на 18 сентября 9:30") {
		t.Fatal("new explicit booking request must replace the active draft")
	}
	if isExplicitAdminBookingRequest("воск ноги до колен") {
		t.Fatal("a service-selection answer must continue the current draft")
	}
}

func TestHandleMessageExplicitBookingReplacesStaleDraftDate(t *testing.T) {
	oldStart := "2099-09-15T10:20:00+03:00"
	newStart := "2099-09-18T09:30:00+03:00"
	start, err := time.Parse(time.RFC3339, newStart)
	if err != nil {
		t.Fatal(err)
	}
	store := &adminBookingRestartStore{
		state: ConversationState{
			Step:           conversationStepCategory,
			BookingDraft:   "admin",
			Username:       "old-client",
			ContactType:    "telegram",
			FromDateTime:   oldStart,
			ServiceIndexes: []int{1},
		},
		services: []ServiceView{{Name: "Воск ноги до колен, руки", DurationMin: 15}},
		slots: []AvailabilitySlot{{
			StartAt: start, EndAt: start.Add(15 * time.Minute), ServiceNames: []string{"Воск ноги до колен, руки"},
		}},
	}
	parser := &adminBookingRestartParser{intent: nlu.AdminBookingIntent{
		IsCreateBooking: true,
		ContactType:     "telegram",
		Contact:         "@nicole",
		ServiceIndexes:  []int{1},
		DurationMin:     15,
		StartAt:         newStart,
		Confidence:      0.99,
	}}
	tg := &adminBookingRestartTelegram{}
	bookingBot := New(tg, store, nil)
	bookingBot.SetAdminBookingIntentParser(parser)

	err = bookingBot.HandleMessage(context.Background(), &telegram.Message{
		From: telegram.User{ID: 42, Username: "master"},
		Chat: telegram.Chat{ID: 42},
		Text: "Запиши Николь воск ноги до колен, руки на 18 сентября 9:30",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if parser.calls != 1 {
		t.Fatalf("admin booking parser calls = %d, want 1", parser.calls)
	}
	if store.state.FromDateTime != newStart {
		t.Fatalf("draft start = %q, want new start %q (old start was %q)", store.state.FromDateTime, newStart, oldStart)
	}
	if store.state.Username != "nicole" {
		t.Fatalf("draft client = %q, want nicole", store.state.Username)
	}
}

type adminBookingRestartStore struct {
	Store
	state    ConversationState
	services []ServiceView
	slots    []AvailabilitySlot
}

func (s *adminBookingRestartStore) RegisterOrUpdateUser(context.Context, UserRecord) (UserRecord, error) {
	return UserRecord{TelegramID: 42, Username: "master", Role: RoleAdmin, Language: LangRU}, nil
}

func (s *adminBookingRestartStore) GetConversationState(context.Context, int64) (ConversationState, error) {
	return s.state, nil
}

func (s *adminBookingRestartStore) SetConversationState(_ context.Context, _ int64, state ConversationState) error {
	s.state = state
	return nil
}

func (s *adminBookingRestartStore) ListServices(context.Context, int64) ([]ServiceView, error) {
	return s.services, nil
}

func (s *adminBookingRestartStore) ListContactAliases(context.Context, int64) ([]ContactAlias, error) {
	return nil, nil
}

func (s *adminBookingRestartStore) ListFreeSlotsForServicesRange(context.Context, int64, []int, time.Time, time.Time) ([]AvailabilitySlot, error) {
	return s.slots, nil
}

func (s *adminBookingRestartStore) ListCachedAvailability(context.Context, int64) ([]AvailabilitySlot, error) {
	return s.slots, nil
}

type adminBookingRestartParser struct {
	intent nlu.AdminBookingIntent
	calls  int
}

func (p *adminBookingRestartParser) ParseAdminBookingIntent(context.Context, nlu.AdminBookingIntentRequest) (nlu.AdminBookingIntent, error) {
	p.calls++
	return p.intent, nil
}

type adminBookingRestartTelegram struct {
	TelegramClient
}

func (adminBookingRestartTelegram) SendMessage(context.Context, telegram.SendMessageRequest) error {
	return nil
}

func TestNormalizeAdminBookingContact(t *testing.T) {
	contactType, contact := normalizeAdminBookingContact("telegram", "@Client")
	if contactType != "telegram" || contact != "client" {
		t.Fatalf("telegram contact = %q, %q", contactType, contact)
	}
	contactType, contact = normalizeAdminBookingContact("unknown", "+357 99 999999")
	if contactType != "phone" || contact != "+35799999999" {
		t.Fatalf("phone contact = %q, %q", contactType, contact)
	}
}

func TestParseAdminBookingStart(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	got, err := parseAdminBookingStart("2026-08-16T18:00:00+03:00", loc)
	if err != nil {
		t.Fatalf("parseAdminBookingStart: %v", err)
	}
	want := time.Date(2026, 8, 16, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("start = %s, want %s", got, want)
	}
}

func TestAdminBookingDurationMustMatchSelectedServices(t *testing.T) {
	services := []ServiceView{
		{Name: "Эпиляция", DurationMin: 60},
		{Name: "Эпиляция", DurationMin: 90},
	}
	if adminBookingDurationMatches([]int{1}, 90, services) {
		t.Fatal("60 minute service must not satisfy a 90 minute request")
	}
	if !adminBookingDurationMatches([]int{2}, 90, services) {
		t.Fatal("90 minute service should satisfy a 90 minute request")
	}
	if !adminBookingDurationMatches([]int{1, 2}, 150, services) {
		t.Fatal("combined service duration should be accepted")
	}
}
