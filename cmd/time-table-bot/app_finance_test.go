package main

import (
	"testing"
	"time"

	"time-table-bot/internal/domain"
)

func TestServiceFinancePrice(t *testing.T) {
	tests := []struct {
		name    string
		service domain.AdminService
		cents   int64
		reason  string
	}{
		{name: "euro in name", service: domain.AdminService{Name: "1 час 45 €"}, cents: 4500},
		{name: "description", service: domain.AdminService{Name: "Усы", Description: "10 евро"}, cents: 1000},
		{name: "trailing amount", service: domain.AdminService{Name: "Тело и лицо 360"}, cents: 36000},
		{name: "structured price", service: domain.AdminService{Name: "Усы", PriceCents: 1200}, cents: 1200},
		{name: "missing", service: domain.AdminService{Name: "Усы"}, reason: "price_missing"},
		{name: "course", service: domain.AdminService{Subcategory: "Курс из 6 процедур", Name: "Тело 300€"}, reason: "package_price"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cents, reason := serviceFinancePrice(tt.service)
			if cents != tt.cents || reason != tt.reason {
				t.Fatalf("price = %d, %q; want %d, %q", cents, reason, tt.cents, tt.reason)
			}
		})
	}
}

func TestBookingFinanceAmountSumsServices(t *testing.T) {
	services := map[int64]domain.AdminService{
		1: {ID: 1, Name: "Бикини 25€"},
		2: {ID: 2, Name: "Усы", Description: "10€"},
	}
	got, reason := bookingFinanceAmount([]int64{1, 2}, services)
	if reason != "" || got != 3500 {
		t.Fatalf("amount = %d, %q; want 3500", got, reason)
	}
}

func TestFinanceBucketsUseDaysForMonth(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	buckets := financeBuckets(from, from.AddDate(0, 1, 0), "month")
	if len(buckets) != 31 || buckets[0].Label != "01" || buckets[30].Label != "31" {
		t.Fatalf("buckets = %#v", buckets)
	}
}
