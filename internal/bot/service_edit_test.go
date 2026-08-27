package bot

import (
	"strings"
	"testing"
)

func TestParseServiceEditDurationSupportsVoiceFriendlyValues(t *testing.T) {
	tests := map[string]int{
		"45":             45,
		"45 минут":       45,
		"1,5 часа":       90,
		"1 час 30 минут": 90,
		"полтора часа":   90,
		"2 hours":        120,
		"one and a half": 90,
	}
	for input, want := range tests {
		got, ok := parseServiceEditDuration(input)
		if !ok || got != want {
			t.Errorf("parseServiceEditDuration(%q) = %d, %v; want %d, true", input, got, ok, want)
		}
	}
}

func TestParseServiceEditDataKeepsDescriptionOptional(t *testing.T) {
	_, _, description, hasDescription, ok := parseServiceEditDataPatch("45 Ногти > Маникюр > Классический")
	if !ok || hasDescription || description != "" {
		t.Fatalf("patch without description = %q, %v, %v", description, hasDescription, ok)
	}
	_, _, description, hasDescription, ok = parseServiceEditDataPatch("45 Ногти > Маникюр > Классический | -")
	if !ok || !hasDescription || description != "" {
		t.Fatalf("explicitly cleared description = %q, %v, %v", description, hasDescription, ok)
	}
}

func TestServiceEditKeyboardsGuideTargetedChange(t *testing.T) {
	services := []ServiceView{{Category: "Ногти", Name: "Маникюр", DurationMin: 45}}
	choose := serviceEditServiceKeyboard(LangRU, services)
	if got := choose.InlineKeyboard[0][0].CallbackData; got != "serviceedit:pick:1" {
		t.Fatalf("service callback = %q", got)
	}
	fields := serviceEditFieldKeyboard(LangRU)
	callbacks := make([]string, 0)
	for _, row := range fields.InlineKeyboard {
		for _, button := range row {
			callbacks = append(callbacks, button.CallbackData)
		}
	}
	joined := strings.Join(callbacks, ",")
	for _, want := range []string{
		"serviceedit:field:price", "serviceedit:field:description", "serviceedit:field:duration",
		"serviceedit:field:name", "serviceedit:field:category", "serviceedit:field:subcategory",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("field callbacks = %q, missing %q", joined, want)
		}
	}
}

func TestNormalizeServicePriceUsesCurrencySymbols(t *testing.T) {
	tests := map[string]string{
		"50 евро":      "50 €",
		"20 Евро":      "20 €",
		"15 EUR":       "15 €",
		"70 Euro":      "70 €",
		"70 Euros":     "70 €",
		"70 €o":        "70 €",
		"30 долларов":  "30 $",
		"25 USD":       "25 $",
		"40 pounds":    "40 £",
		"1 500 рублей": "1 500 ₽",
	}
	for input, want := range tests {
		if got := normalizeServicePrice(input); got != want {
			t.Errorf("normalizeServicePrice(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestSplitTrailingServicePrice(t *testing.T) {
	tests := []struct {
		input string
		name  string
		price string
	}{
		{"Только тело 55€", "Только тело", "55 €"},
		{"Тело и лицо 65 EUR", "Тело и лицо", "65 €"},
		{"Массаж 90 минут", "Массаж 90 минут", ""},
	}
	for _, test := range tests {
		name, price := splitTrailingServicePrice(test.input)
		if name != test.name || price != test.price {
			t.Errorf("splitTrailingServicePrice(%q) = %q, %q; want %q, %q", test.input, name, price, test.name, test.price)
		}
	}
}

func TestServiceEditCategoryKeyboardsUseExistingValues(t *testing.T) {
	services := []ServiceView{
		{Category: "Ногти", Subcategory: "Маникюр"},
		{Category: "Ногти", Subcategory: "Педикюр"},
		{Category: "Брови", Subcategory: "Коррекция"},
	}
	categoryKeyboard := serviceEditFieldInputKeyboard(LangRU, serviceEditFieldCategory, services, services[0])
	if got := categoryKeyboard.InlineKeyboard[0][0].Text; got != "Ногти" {
		t.Fatalf("first category = %q", got)
	}
	subcategoryKeyboard := serviceEditFieldInputKeyboard(LangRU, serviceEditFieldSubcategory, services, services[0])
	if got := subcategoryKeyboard.InlineKeyboard[1][0].Text; got != "Педикюр" {
		t.Fatalf("second subcategory = %q", got)
	}
}

func TestParseServiceEditSections(t *testing.T) {
	category, subcategory, ok := parseServiceEditSections("Эпиляция > Лицо")
	if !ok || category != "Эпиляция" || subcategory != "Лицо" {
		t.Fatalf("sections = %q, %q, %v", category, subcategory, ok)
	}
	category, subcategory, ok = parseServiceEditSections("-")
	if !ok || category != "" || subcategory != "" {
		t.Fatalf("cleared sections = %q, %q, %v", category, subcategory, ok)
	}
}
