package main

import "testing"

func TestParseServicePath(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		wantCategory    string
		wantSubcategory string
		wantName        string
	}{
		{
			name:     "legacy flat service",
			raw:      "Маникюр",
			wantName: "Маникюр",
		},
		{
			name:            "category subcategory service",
			raw:             "Ногти > Маникюр > Классический",
			wantCategory:    "Ногти",
			wantSubcategory: "Маникюр",
			wantName:        "Классический",
		},
		{
			name:         "category service",
			raw:          "Ногти > Маникюр",
			wantCategory: "Ногти",
			wantName:     "Маникюр",
		},
		{
			name:            "pipe separator",
			raw:             "Nails | Pedicure | Classic",
			wantCategory:    "Nails",
			wantSubcategory: "Pedicure",
			wantName:        "Classic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, subcategory, name := parseServicePath(tt.raw)
			if category != tt.wantCategory || subcategory != tt.wantSubcategory || name != tt.wantName {
				t.Fatalf("parseServicePath(%q) = %q, %q, %q; want %q, %q, %q", tt.raw, category, subcategory, name, tt.wantCategory, tt.wantSubcategory, tt.wantName)
			}
		})
	}
}

func TestMasterServicesTextUsesMasterTelegram(t *testing.T) {
	raw := "Прайс\nЕсли вас интересуте какая-либо из услуг, обращайтесь: @old_contact\nКатегории:"
	got := masterServicesText(raw, "master")
	want := "Прайс\nЕсли вас интересует какая-либо из услуг, обращайтесь: @master\nКатегории:"
	if got != want {
		t.Fatalf("masterServicesText() = %q, want %q", got, want)
	}
}
