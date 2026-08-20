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

func TestLooksLikeServiceCatalogReplace(t *testing.T) {
	for _, text := range []string{
		"Измени список услуг: эпиляция 30 минут 25 евро",
		"Замени весь прайс на новый",
		"Replace service list: manicure 45 minutes 40 EUR",
	} {
		if !looksLikeAdminServiceImport(text) || !looksLikeServiceCatalogReplace(text) {
			t.Fatalf("catalog replacement was not detected: %q", text)
		}
	}
	for _, text := range []string{
		"Добавь услугу: эпиляция 30 минут 25 евро",
		"Обнови прайс для маникюра",
	} {
		if looksLikeServiceCatalogReplace(text) {
			t.Fatalf("partial service update was detected as replacement: %q", text)
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

func TestEvaluateServiceImportRejectsDuplicates(t *testing.T) {
	items := evaluateServiceImport(LangRU, []ServiceImportDraft{
		{Name: "Маникюр", DurationMin: 45, Confidence: 0.9},
		{Name: " маникюр ", DurationMin: 45, Confidence: 0.9},
	})
	if len(items) != 2 || !items[0].Ready || items[1].Ready || !strings.Contains(items[1].Issue, "повтор") {
		t.Fatalf("duplicate evaluation = %#v", items)
	}
}

func TestFormatServiceImportPreviewAndKeyboard(t *testing.T) {
	items := evaluateServiceImport(LangRU, []ServiceImportDraft{{
		Category: "Электроэпиляция", Subcategory: "По времени", Name: "1,5 часа",
		DurationMin: 90, PriceText: "60 €", Confidence: 0.95,
	}})
	preview := formatServiceImportPreview(LangRU, items, false)
	if !strings.Contains(preview, "+ Электроэпиляция > По времени > 1,5 часа — 90 мин. — 60 €") {
		t.Fatalf("preview = %q", preview)
	}
	kb := serviceImportKeyboard(LangRU, true, false)
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "serviceimport:yes" {
		t.Fatalf("confirm callback = %q", got)
	}
}

func TestFormatServiceReplacePreviewAndKeyboard(t *testing.T) {
	items := evaluateServiceImport(LangRU, []ServiceImportDraft{{
		Category: "Электроэпиляция", Name: "1 час", DurationMin: 60, PriceText: "45 €", Confidence: 0.95,
	}})
	preview := formatServiceImportPreview(LangRU, items, true)
	for _, want := range []string{"Новый список услуг для клиентов:", "целиком заменит текущий", "старые записи сохранятся"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("replacement preview = %q, missing %q", preview, want)
		}
	}
	kb := serviceImportKeyboard(LangRU, true, true)
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "serviceimport:yes" {
		t.Fatalf("confirm callback = %q", got)
	}
	if got := kb.InlineKeyboard[1][0].CallbackData; got != "serviceimport:edit" {
		t.Fatalf("edit callback = %q", got)
	}
}
