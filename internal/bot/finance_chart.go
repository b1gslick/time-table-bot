package bot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"
)

const (
	financeChartWidth  = 1400
	financeChartHeight = 900
)

var (
	financeIncomeColor  = color.RGBA{R: 52, G: 143, B: 92, A: 255}
	financeExpenseColor = color.RGBA{R: 211, G: 91, B: 74, A: 255}
	financeAxisColor    = color.RGBA{R: 96, G: 108, B: 113, A: 255}
)

func renderFinanceChart(lang string, report FinanceReport) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, financeChartWidth, financeChartHeight))
	fillRect(img, img.Bounds(), scheduleBg)
	fonts, err := newScheduleFontSet()
	if err != nil {
		return nil, err
	}
	defer fonts.close()

	fillRect(img, image.Rect(36, 36, financeChartWidth-36, financeChartHeight-36), schedulePanel)
	title := tr(lang, "finance_chart_title", report.From.Format("02.01.2006"), report.To.Add(-time.Nanosecond).Format("02.01.2006"))
	drawFontText(img, fonts.title, 76, 92, title, scheduleText)
	income := report.BookingIncomeCents + report.ManualIncomeCents
	net := income - report.ExpenseCents
	drawFontText(img, fonts.day, 76, 140, tr(lang, "finance_chart_income", formatMoney(income, report.Currency)), financeIncomeColor)
	drawFontText(img, fonts.day, 470, 140, tr(lang, "finance_chart_expenses", formatMoney(report.ExpenseCents, report.Currency)), financeExpenseColor)
	drawFontText(img, fonts.day, 900, 140, tr(lang, "finance_chart_net", formatMoney(net, report.Currency)), scheduleText)

	plotLeft, plotRight := 92, financeChartWidth-70
	plotTop, plotBottom := 205, financeChartHeight-125
	drawLineH(img, plotLeft, plotRight, plotBottom, financeAxisColor)
	drawLineV(img, plotLeft, plotTop, plotBottom, financeAxisColor)
	maxValue := int64(1)
	for _, bucket := range report.Buckets {
		if bucket.IncomeCents > maxValue {
			maxValue = bucket.IncomeCents
		}
		if bucket.ExpenseCents > maxValue {
			maxValue = bucket.ExpenseCents
		}
	}
	for i := 0; i <= 4; i++ {
		y := plotBottom - (plotBottom-plotTop)*i/4
		drawLineH(img, plotLeft, plotRight, y, scheduleGridSoft)
		value := maxValue * int64(i) / 4
		drawFontText(img, fonts.small, 42, y+5, compactMoney(value), scheduleMutedText)
	}
	if len(report.Buckets) > 0 {
		groupWidth := (plotRight - plotLeft) / len(report.Buckets)
		barWidth := groupWidth / 3
		if barWidth < 3 {
			barWidth = 3
		}
		if barWidth > 26 {
			barWidth = 26
		}
		labelEvery := 1
		if len(report.Buckets) > 16 {
			labelEvery = 3
		}
		for i, bucket := range report.Buckets {
			center := plotLeft + i*groupWidth + groupWidth/2
			incomeHeight := int(bucket.IncomeCents * int64(plotBottom-plotTop) / maxValue)
			expenseHeight := int(bucket.ExpenseCents * int64(plotBottom-plotTop) / maxValue)
			fillRect(img, image.Rect(center-barWidth-1, plotBottom-incomeHeight, center-1, plotBottom), financeIncomeColor)
			fillRect(img, image.Rect(center+1, plotBottom-expenseHeight, center+barWidth+1, plotBottom), financeExpenseColor)
			if i%labelEvery == 0 || i == len(report.Buckets)-1 {
				drawCenteredFontText(img, fonts.small, center-groupWidth/2, center+groupWidth/2, plotBottom+28, bucket.Label, scheduleMutedText)
			}
		}
	}
	legendY := financeChartHeight - 72
	fillRect(img, image.Rect(82, legendY-16, 106, legendY+8), financeIncomeColor)
	drawFontText(img, fonts.body, 118, legendY+5, tr(lang, "finance_kind_income"), scheduleText)
	fillRect(img, image.Rect(300, legendY-16, 324, legendY+8), financeExpenseColor)
	drawFontText(img, fonts.body, 336, legendY+5, tr(lang, "finance_kind_expense"), scheduleText)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compactMoney(cents int64) string {
	if cents >= 100000000 {
		return fmt.Sprintf("%.1fM", float64(cents)/100000000)
	}
	if cents >= 100000 {
		return fmt.Sprintf("%.1fk", float64(cents)/100000)
	}
	return fmt.Sprintf("%d", cents/100)
}

func financeChartCaption(lang string, report FinanceReport) string {
	income := report.BookingIncomeCents + report.ManualIncomeCents
	return tr(lang, "finance_chart_caption", formatMoney(income, report.Currency), formatMoney(report.ExpenseCents, report.Currency), formatMoney(income-report.ExpenseCents, report.Currency))
}
