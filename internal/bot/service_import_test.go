package bot

import (
	"strings"
	"testing"
)

func TestLooksLikeAdminServiceImport(t *testing.T) {
	for _, text := range []string{
		"Добавь услуги: электроэпиляция час 45 евро, полтора часа 60 евро",
		"Электроэпиляция 1 час 45 €, полтора часа 60 €",
		"Add services: wax upper lip 15 minutes 10 EUR",
	} {
		if !looksLikeAdminServiceImport(text) {
			t.Fatalf("service import was not detected: %q", text)
		}
	}
	for _, text := range []string{
		"запиши Лизу на электроэпиляцию час 45 евро завтра",
		"покажи услуги",
	} {
		if looksLikeAdminServiceImport(text) {
			t.Fatalf("non-import text was detected: %q", text)
		}
	}
}

func TestEvaluateServiceImportBuildsCategoryPath(t *testing.T) {
	items := evaluateServiceImport(LangRU, []ServiceImportDraft{
		{Category: " Восковая  депиляция ", Subcategory: "Лицо", Name: "Усы", DurationMin: 15, PriceText: "10 €", Confidence: 0.95},
		{Name: "Без времени", Confidence: 0.9},
	})
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if !items[0].Ready || items[0].Path != "Восковая депиляция > Лицо > Усы" {
		t.Fatalf("ready item = %#v", items[0])
	}
	if items[1].Ready || !strings.Contains(items[1].Issue, "длительность") {
		t.Fatalf("invalid item = %#v", items[1])
	}
}

func TestFormatServiceImportPreviewAndKeyboard(t *testing.T) {
	items := evaluateServiceImport(LangRU, []ServiceImportDraft{{
		Category: "Электроэпиляция", Subcategory: "По времени", Name: "1,5 часа",
		DurationMin: 90, PriceText: "60 €", Confidence: 0.95,
	}})
	preview := formatServiceImportPreview(LangRU, items)
	if !strings.Contains(preview, "+ Электроэпиляция > По времени > 1,5 часа — 90 мин. — 60 €") {
		t.Fatalf("preview = %q", preview)
	}
	kb := serviceImportKeyboard(LangRU, true)
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "serviceimport:yes" {
		t.Fatalf("confirm callback = %q", got)
	}
}
