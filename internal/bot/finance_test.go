package bot

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestFinanceReportPeriodFromSpecificMonth(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.Local)
	for text, want := range map[string]string{
		"сделай отчет за май":           "2026-05",
		"сделай отчет за декабрь":       "2025-12",
		"отчет за май 2025":             "2025-05",
		"посчитай за этот месяц":        "month",
		"финансовый отчет за 2 квартал": "2026-Q2",
		"отчет за 2025 год":             "2025",
	} {
		got, ok := financeReportPeriodFromText(text, now)
		if !ok || got != want {
			t.Fatalf("period for %q = %q, %v; want %q", text, got, ok, want)
		}
	}
}

func TestFinanceRangeForSpecificMonth(t *testing.T) {
	from, to, bucket, ok := financeRangeForPeriod("2026-05", time.Now())
	if !ok || from.Format("2006-01-02") != "2026-05-01" || to.Format("2006-01-02") != "2026-06-01" || bucket != "month" {
		t.Fatalf("range = %s..%s %q %v", from, to, bucket, ok)
	}
}

func TestFinanceChartRequestNeedsFinanceContext(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.Local)
	for text, want := range map[string]string{
		"покажи финансовый график доходов за май": "2026-05",
		"покажи график расходов":                  "month",
		"покажи график на следующую неделю":       "",
	} {
		got, ok := financeChartPeriodFromText(text, now)
		if got != want || ok != (want != "") {
			t.Fatalf("finance chart period for %q = %q, %v; want %q", text, got, ok, want)
		}
	}
}

func TestLooksLikeFinanceInputIsAdminOperationOnly(t *testing.T) {
	if !looksLikeFinanceInput("аренда 600 евро", "text", "") {
		t.Fatal("expense was not detected")
	}
	if !looksLikeFinanceInput("ИТОГО 85.40 EUR", "image", "") {
		t.Fatal("receipt was not detected")
	}
	if !looksLikeFinanceInput("аренда шестьсот евро", "voice", "") {
		t.Fatal("spoken-word expense was not detected")
	}
	if looksLikeFinanceInput("запиши Лизу на услугу 45 евро", "text", "") {
		t.Fatal("booking was detected as finance input")
	}
}

func TestFormatFinanceReportIncludesUnresolved(t *testing.T) {
	report := FinanceReport{
		From: time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local), To: time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local), Currency: "EUR",
		BookingIncomeCents: 10000, ManualIncomeCents: 2000, ExpenseCents: 3000,
		ExpenseCategories: map[string]int64{"rent": 3000},
		Unresolved:        []FinanceUnresolved{{StartAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.Local), Client: "Лиза", ServiceNames: []string{"Усы"}, Reason: "price_missing"}},
	}
	got := formatFinanceReport(LangRU, report)
	for _, want := range []string{"Всего доходов: 120.00 EUR", "Расходы: 30.00 EUR", "Итого после расходов: 90.00 EUR", "Лиза", "цена не указана"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report %q does not contain %q", got, want)
		}
	}
}

func TestRenderFinanceChartProducesPNG(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	image, err := renderFinanceChart(LangRU, FinanceReport{
		From: from, To: from.AddDate(0, 1, 0), Currency: "EUR",
		BookingIncomeCents: 4500, ExpenseCents: 1000,
		Buckets: []FinanceBucket{{StartAt: from, Label: "01", IncomeCents: 4500, ExpenseCents: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil || cfg.Width != financeChartWidth || cfg.Height != financeChartHeight {
		t.Fatalf("chart config = %#v, %v", cfg, err)
	}
}
