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
	for _, want := range []string{"serviceedit:field:price", "serviceedit:field:duration", "serviceedit:field:name", "serviceedit:field:sections"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("field callbacks = %q, missing %q", joined, want)
		}
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
