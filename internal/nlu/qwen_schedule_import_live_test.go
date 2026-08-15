package nlu

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestQwenScheduleImportLive(t *testing.T) {
	if os.Getenv("QWEN_LIVE_TEST") != "1" {
		t.Skip("set QWEN_LIVE_TEST=1 to run against Qwen Cloud")
	}
	parser, err := NewQwenParser(QwenConfig{
		APIKey: os.Getenv("QWEN_API_KEY"), BaseURL: os.Getenv("QWEN_BASE_URL"),
		Model: os.Getenv("QWEN_MODEL"), Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ocr := `Авг. 2026 г.
15
Неделя 34
9:30 Анастасия S
электро 1 час
10:40 Катя Америка
Электро и воск
13:00 Полина Колзина
1 час
пн
14:40 Кейт Катерина
чт
17
воск
20
9:30 Стефани электро
12:00 Мира бикини
12:40 Маша бачата
электро 1,5 ч и бикини
воск
вт
пт
18
21
9:45 Марина Фаринюк
1 час электро
11:00 Влада Барсук 1
час электро
22
12:00 Машенька
электро подмышки
ср
1 час
19
13:40 Стефани 2 с
электро
16:00 Джессика
бикини
вс
16:50 Анастасия
23
Балтаджи подмышки
17:00 эндо Наташа`
	if imagePath := strings.TrimSpace(os.Getenv("OCR_IMAGE")); imagePath != "" {
		image, readErr := os.ReadFile(imagePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		recognizer, recognizeErr := NewTesseractRecognizer(TesseractConfig{
			CLIPath: os.Getenv("TESSERACT_CLI_PATH"), Languages: os.Getenv("TESSERACT_LANGUAGES"), Timeout: 45 * time.Second,
		})
		if recognizeErr != nil {
			t.Fatal(recognizeErr)
		}
		_, layout, recognizeErr := recognizer.RecognizeTextWithLayout(context.Background(), ImageTextRequest{Image: image, MIMEType: "image/png", Language: "ru"})
		if recognizeErr != nil {
			t.Fatal(recognizeErr)
		}
		ocr = layout
	}
	intent, err := parser.ParseAdminScheduleImport(context.Background(), AdminScheduleImportRequest{
		Text: ocr, Language: "ru",
		Now:      time.Date(2026, 8, 15, 15, 41, 0, 0, time.FixedZone("Europe/Nicosia", 3*60*60)),
		Timezone: "Europe/Nicosia",
		Services: []Service{
			{Index: 1, Category: "Восковая депиляция", Name: "Бикини классика", DurationMin: 15},
			{Index: 2, Category: "Восковая депиляция", Name: "Бикини глубокое", DurationMin: 30},
			{Index: 3, Category: "Восковая депиляция", Name: "Лицо полностью", DurationMin: 15},
			{Index: 4, Category: "Восковая депиляция", Name: "Ноги полностью", DurationMin: 30},
			{Index: 5, Category: "Восковая депиляция", Name: "Руки полностью", DurationMin: 20},
			{Index: 6, Category: "Электроэпиляция", Name: "До 30 мин", DurationMin: 30},
			{Index: 7, Category: "Электроэпиляция", Name: "1 час", DurationMin: 60},
			{Index: 8, Category: "Электроэпиляция", Name: "2 часа", DurationMin: 120},
			{Index: 9, Category: "Эндосфера", Subcategory: "Разовая процедура", Name: "Только тело", DurationMin: 50},
			{Index: 10, Category: "Эндосфера", Subcategory: "Разовая процедура", Name: "Тело и лицо", DurationMin: 65},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.IsSchedule || len(intent.Entries) < 10 {
		t.Fatalf("schedule=%v confidence=%.2f entries=%d intent=%+v", intent.IsSchedule, intent.Confidence, len(intent.Entries), intent)
	}
	t.Logf("schedule confidence=%.2f entries=%d", intent.Confidence, len(intent.Entries))
	for _, entry := range intent.Entries {
		t.Logf("%s | %s | %v | %v", entry.StartAt, entry.Client, entry.ServiceIndexes, entry.ServiceQueries)
	}
}

func TestQwenServiceImportLive(t *testing.T) {
	if os.Getenv("QWEN_LIVE_TEST") != "1" {
		t.Skip("set QWEN_LIVE_TEST=1 to run against Qwen Cloud")
	}
	parser, err := NewQwenParser(QwenConfig{
		APIKey: os.Getenv("QWEN_API_KEY"), BaseURL: os.Getenv("QWEN_BASE_URL"),
		Model: os.Getenv("QWEN_MODEL"), Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parser.ParseAdminServiceImport(context.Background(), AdminServiceImportRequest{
		Text:     "Добавь услуги: электроэпиляция один час 45 евро, полтора часа 60 евро; воск, усы 10 евро 15 минут и глубокое бикини 30 евро 30 минут",
		Language: "ru",
		ExistingServices: []Service{{
			Category: "Восковая депиляция", Subcategory: "Лицо", Name: "Лицо полностью", DurationMin: 15,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.IsServiceCatalog || len(intent.Entries) != 4 {
		t.Fatalf("catalog=%v confidence=%.2f entries=%d intent=%+v", intent.IsServiceCatalog, intent.Confidence, len(intent.Entries), intent)
	}
	for _, entry := range intent.Entries {
		if entry.Name == "" || entry.Category == "" || entry.DurationMin <= 0 || entry.PriceText == "" {
			t.Fatalf("incomplete service entry: %+v", entry)
		}
	}
	t.Logf("service catalog confidence=%.2f entries=%+v", intent.Confidence, intent.Entries)
}

func TestQwenFinanceIntentLive(t *testing.T) {
	if os.Getenv("QWEN_LIVE_TEST") != "1" {
		t.Skip("set QWEN_LIVE_TEST=1 to run against Qwen Cloud")
	}
	parser, err := NewQwenParser(QwenConfig{
		APIKey: os.Getenv("QWEN_API_KEY"), BaseURL: os.Getenv("QWEN_BASE_URL"),
		Model: os.Getenv("QWEN_MODEL"), Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parser.ParseAdminFinanceIntent(context.Background(), AdminFinanceIntentRequest{
		Text:     "Сегодня купила расходники на восемьдесят пять евро сорок центов и заплатила аренду шестьсот евро",
		Language: "ru", Source: "voice",
		Now:      time.Date(2026, 8, 15, 12, 0, 0, 0, time.FixedZone("Europe/Nicosia", 3*60*60)),
		Timezone: "Europe/Nicosia", ForcedKind: "expense",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.IsFinance || len(intent.Entries) != 2 {
		t.Fatalf("finance=%v confidence=%.2f entries=%d", intent.IsFinance, intent.Confidence, len(intent.Entries))
	}
	amounts := map[int64]bool{}
	for _, entry := range intent.Entries {
		if entry.Kind != "expense" || entry.Currency != "EUR" {
			t.Fatalf("unexpected entry: %+v", entry)
		}
		amounts[entry.AmountCents] = true
	}
	if !amounts[8540] || !amounts[60000] {
		t.Fatalf("amounts = %v, want 8540 and 60000 cents", amounts)
	}
}
