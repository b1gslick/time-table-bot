package bot

import (
	"testing"
	"time"
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
