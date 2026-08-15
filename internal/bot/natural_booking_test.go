package bot

import (
	"testing"
	"time"

	"time-table-bot/internal/nlu"
)

func TestResolveNaturalBookingServicesMatchesRussianInflection(t *testing.T) {
	services := []ServiceView{
		{Name: "Эпиляция", DurationMin: 90},
		{Name: "Маникюр", DurationMin: 60},
	}
	intent := nlu.BookingIntent{
		IsBooking:      true,
		ServiceQueries: []string{"эпиляцию"},
		DurationMin:    90,
		Confidence:     0.9,
	}
	got := resolveNaturalBookingServices(intent, "хочу эпиляцию завтра вечером", services)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("service indexes = %#v, want [1]", got)
	}
}

func TestResolveNaturalBookingServicesUsesDurationAsTieBreaker(t *testing.T) {
	services := []ServiceView{
		{Name: "Коррекция бровей", DurationMin: 30},
		{Name: "Архитектура бровей", DurationMin: 60},
	}
	intent := nlu.BookingIntent{
		IsBooking:      true,
		ServiceQueries: []string{"брови"},
		DurationMin:    60,
		Confidence:     0.9,
	}
	got := resolveNaturalBookingServices(intent, "можно на брови на час", services)
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("service indexes = %#v, want [2]", got)
	}
}

func TestNaturalBookingRangeUsesExclusiveDateTo(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	now := time.Date(2026, 8, 15, 10, 30, 0, 0, loc)
	from, to := naturalBookingRange(nlu.BookingIntent{
		DateFrom: "2026-08-16",
		DateTo:   "2026-08-17",
	}, now)
	if !from.Equal(time.Date(2026, 8, 16, 0, 0, 0, 0, loc)) {
		t.Fatalf("from = %s", from)
	}
	if !to.Equal(time.Date(2026, 8, 17, 0, 0, 0, 0, loc)) {
		t.Fatalf("to = %s", to)
	}
}

func TestNaturalBookingRangeKeepsTodayFromNow(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	now := time.Date(2026, 8, 15, 10, 30, 0, 0, loc)
	from, to := naturalBookingRange(nlu.BookingIntent{
		DateFrom: "2026-08-15",
		DateTo:   "2026-08-16",
	}, now)
	if !from.Equal(now) {
		t.Fatalf("from = %s, want now", from)
	}
	if !to.Equal(time.Date(2026, 8, 16, 0, 0, 0, 0, loc)) {
		t.Fatalf("to = %s", to)
	}
}
