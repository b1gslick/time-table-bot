package bot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"sort"
	"strings"
	"time"
)

const (
	scheduleImageWidth  = 1400
	schedulePanelHeight = 1120
)

var (
	scheduleBg        = color.RGBA{R: 245, G: 247, B: 244, A: 255}
	schedulePanel     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	scheduleGrid      = color.RGBA{R: 217, G: 224, B: 226, A: 255}
	scheduleGridSoft  = color.RGBA{R: 238, G: 242, B: 243, A: 255}
	scheduleText      = color.RGBA{R: 36, G: 43, B: 50, A: 255}
	scheduleMutedText = color.RGBA{R: 111, G: 122, B: 132, A: 255}
	scheduleFree      = color.RGBA{R: 105, G: 184, B: 125, A: 255}
	scheduleBooked    = color.RGBA{R: 211, G: 103, B: 103, A: 255}
	scheduleClosed    = color.RGBA{R: 142, G: 151, B: 160, A: 255}
	schedulePartial   = color.RGBA{R: 229, G: 171, B: 79, A: 255}
	scheduleWhite     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

type scheduleSlotGroup struct {
	Name  string
	Slots []ScheduleGridSlot
}

func renderScheduleWeekImage(lang string, start time.Time, slots []ScheduleGridSlot) ([]byte, error) {
	start = weekStart(start)
	groups := groupScheduleSlots(slots)
	height := 80 + len(groups)*schedulePanelHeight + 40
	img := image.NewRGBA(image.Rect(0, 0, scheduleImageWidth, height))
	fillRect(img, img.Bounds(), scheduleBg)

	y := 48
	for _, group := range groups {
		drawScheduleWeekPanel(img, lang, start, group, y)
		y += schedulePanelHeight
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func groupScheduleSlots(slots []ScheduleGridSlot) []scheduleSlotGroup {
	hasNames := false
	for _, slot := range slots {
		if strings.TrimSpace(slot.AdminName) != "" {
			hasNames = true
			break
		}
	}
	if !hasNames {
		return []scheduleSlotGroup{{Slots: sortedScheduleSlots(slots)}}
	}

	byName := make(map[string][]ScheduleGridSlot)
	var names []string
	for _, slot := range slots {
		name := strings.TrimSpace(slot.AdminName)
		if name == "" {
			name = "admin"
		}
		if _, ok := byName[name]; !ok {
			names = append(names, name)
		}
		byName[name] = append(byName[name], slot)
	}
	sort.Strings(names)

	groups := make([]scheduleSlotGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, scheduleSlotGroup{Name: name, Slots: sortedScheduleSlots(byName[name])})
	}
	return groups
}

func sortedScheduleSlots(slots []ScheduleGridSlot) []ScheduleGridSlot {
	out := append([]ScheduleGridSlot(nil), slots...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartAt.Equal(out[j].StartAt) {
			return out[i].StartAt.Before(out[j].StartAt)
		}
		return out[i].AdminName < out[j].AdminName
	})
	return out
}

func drawScheduleWeekPanel(img *image.RGBA, lang string, week time.Time, group scheduleSlotGroup, top int) {
	left := 40
	right := scheduleImageWidth - 40
	panelRect := image.Rect(left, top, right, top+schedulePanelHeight-28)
	fillRect(img, panelRect, schedulePanel)
	strokeRect(img, panelRect, scheduleGrid)

	title := "WEEK " + week.Format("02.01") + "-" + week.AddDate(0, 0, 6).Format("02.01")
	if group.Name != "" {
		title = "@" + strings.ToUpper(group.Name) + "  " + title
	}
	drawText(img, left+36, top+32, title, scheduleText, 4)

	minMinute, maxMinute := scheduleMinuteRange(group.Slots)
	timeLeft := left + 34
	gridLeft := left + 130
	gridRight := right - 28
	headerTop := top + 110
	headerHeight := 74
	gridTop := headerTop + headerHeight
	gridBottom := top + schedulePanelHeight - 150
	gridHeight := gridBottom - gridTop
	colWidth := (gridRight - gridLeft) / 7

	for i := 0; i < 7; i++ {
		x1 := gridLeft + i*colWidth
		x2 := x1 + colWidth
		if i == 6 {
			x2 = gridRight
		}
		fill := scheduleWhite
		if i >= 5 {
			fill = color.RGBA{R: 250, G: 249, B: 245, A: 255}
		}
		fillRect(img, image.Rect(x1, headerTop, x2, gridBottom), fill)
		strokeRect(img, image.Rect(x1, headerTop, x2, gridBottom), scheduleGrid)

		day := week.AddDate(0, 0, i)
		label := strings.ToUpper(weekdayShort(lang, day.Weekday())) + " " + day.Format("02")
		drawCenteredText(img, x1, x2, headerTop+20, label, scheduleText, 3)
	}

	for minute := minMinute; minute <= maxMinute; minute += 60 {
		y := minuteToY(minute, minMinute, maxMinute, gridTop, gridHeight)
		drawLineH(img, gridLeft, gridRight, y, scheduleGridSoft)
		drawText(img, timeLeft, y-11, formatMinuteLabel(minute), scheduleMutedText, 2)
	}

	if len(group.Slots) == 0 {
		drawCenteredText(img, gridLeft, gridRight, gridTop+gridHeight/2-20, "NO SCHEDULE", scheduleMutedText, 4)
	} else {
		for _, slot := range group.Slots {
			dayIndex := localDayIndex(week, slot.StartAt)
			if dayIndex < 0 || dayIndex >= 7 {
				continue
			}
			startMinute := minuteOfDay(slot.StartAt)
			endMinute := minuteOfDay(slot.EndAt)
			if endMinute <= startMinute {
				endMinute = startMinute + 30
			}
			x1 := gridLeft + dayIndex*colWidth + 7
			x2 := gridLeft + (dayIndex+1)*colWidth - 7
			if dayIndex == 6 {
				x2 = gridRight - 7
			}
			y1 := minuteToY(startMinute, minMinute, maxMinute, gridTop, gridHeight) + 2
			y2 := minuteToY(endMinute, minMinute, maxMinute, gridTop, gridHeight) - 2
			if y2-y1 < 16 {
				y2 = y1 + 16
			}
			fill := scheduleSlotColor(slot)
			rect := image.Rect(x1, y1, x2, y2)
			fillRect(img, rect, fill)
			strokeRect(img, rect, color.RGBA{R: 255, G: 255, B: 255, A: 210})
			if y2-y1 >= 32 {
				drawCenteredText(img, x1, x2, y1+8, scheduleSlotMarker(slot), scheduleWhite, 3)
			}
		}
	}

	legendY := top + schedulePanelHeight - 100
	drawLegend(img, left+42, legendY)
}

func scheduleMinuteRange(slots []ScheduleGridSlot) (int, int) {
	minMinute := 9 * 60
	maxMinute := 18 * 60
	if len(slots) > 0 {
		minMinute = 24 * 60
		maxMinute = 0
		for _, slot := range slots {
			start := minuteOfDay(slot.StartAt)
			end := minuteOfDay(slot.EndAt)
			if end <= start {
				end = start + 30
			}
			if start < minMinute {
				minMinute = start
			}
			if end > maxMinute {
				maxMinute = end
			}
		}
	}
	minMinute = (minMinute / 60) * 60
	if maxMinute%60 != 0 {
		maxMinute = (maxMinute/60 + 1) * 60
	}
	if maxMinute <= minMinute {
		maxMinute = minMinute + 60
	}
	if maxMinute-minMinute < 4*60 {
		maxMinute = minMinute + 4*60
	}
	if maxMinute > 24*60 {
		maxMinute = 24 * 60
	}
	return minMinute, maxMinute
}

func scheduleSlotColor(slot ScheduleGridSlot) color.RGBA {
	if strings.ToLower(slot.Status) != "open" || slot.Blocked > 0 {
		return scheduleClosed
	}
	if slot.Available > 0 && slot.Booked > 0 {
		return schedulePartial
	}
	if slot.Available > 0 {
		return scheduleFree
	}
	if slot.Booked > 0 {
		return scheduleBooked
	}
	return scheduleClosed
}

func scheduleSlotMarker(slot ScheduleGridSlot) string {
	if strings.ToLower(slot.Status) != "open" || slot.Blocked > 0 {
		return "X"
	}
	if slot.Available > 0 && slot.Booked > 0 {
		return fmt.Sprintf("%d", slot.Available)
	}
	if slot.Available > 0 {
		return "+"
	}
	return "#"
}

func drawLegend(img *image.RGBA, x, y int) {
	items := []struct {
		Label string
		Color color.RGBA
	}{
		{"FREE", scheduleFree},
		{"BOOKED", scheduleBooked},
		{"PARTIAL", schedulePartial},
		{"CLOSED", scheduleClosed},
	}
	for _, item := range items {
		fillRect(img, image.Rect(x, y, x+34, y+34), item.Color)
		strokeRect(img, image.Rect(x, y, x+34, y+34), scheduleGrid)
		drawText(img, x+46, y+5, item.Label, scheduleMutedText, 2)
		x += 230
	}
}

func scheduleWeekCaption(lang string, start time.Time) string {
	return tr(lang, "week_caption", start.Format("02.01"), start.AddDate(0, 0, 6).Format("02.01"))
}

func localDayIndex(weekStart, value time.Time) int {
	start := dateOnly(value.In(weekStart.Location()))
	week := dateOnly(weekStart)
	return int(start.Sub(week).Hours() / 24)
}

func minuteOfDay(value time.Time) int {
	local := value.In(value.Location())
	return local.Hour()*60 + local.Minute()
}

func minuteToY(minute, minMinute, maxMinute, top, height int) int {
	if maxMinute <= minMinute {
		return top
	}
	if minute < minMinute {
		minute = minMinute
	}
	if minute > maxMinute {
		minute = maxMinute
	}
	return top + (minute-minMinute)*height/(maxMinute-minMinute)
}

func formatMinuteLabel(minute int) string {
	if minute >= 24*60 {
		return "24:00"
	}
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

func fillRect(img *image.RGBA, rect image.Rectangle, fill color.RGBA) {
	imagedraw.Draw(img, rect, &image.Uniform{C: fill}, image.Point{}, imagedraw.Src)
}

func strokeRect(img *image.RGBA, rect image.Rectangle, stroke color.RGBA) {
	drawLineH(img, rect.Min.X, rect.Max.X, rect.Min.Y, stroke)
	drawLineH(img, rect.Min.X, rect.Max.X, rect.Max.Y-1, stroke)
	drawLineV(img, rect.Min.X, rect.Min.Y, rect.Max.Y, stroke)
	drawLineV(img, rect.Max.X-1, rect.Min.Y, rect.Max.Y, stroke)
}

func drawLineH(img *image.RGBA, x1, x2, y int, stroke color.RGBA) {
	if y < img.Bounds().Min.Y || y >= img.Bounds().Max.Y {
		return
	}
	if x1 < img.Bounds().Min.X {
		x1 = img.Bounds().Min.X
	}
	if x2 > img.Bounds().Max.X {
		x2 = img.Bounds().Max.X
	}
	for x := x1; x < x2; x++ {
		img.SetRGBA(x, y, stroke)
	}
}

func drawLineV(img *image.RGBA, x, y1, y2 int, stroke color.RGBA) {
	if x < img.Bounds().Min.X || x >= img.Bounds().Max.X {
		return
	}
	if y1 < img.Bounds().Min.Y {
		y1 = img.Bounds().Min.Y
	}
	if y2 > img.Bounds().Max.Y {
		y2 = img.Bounds().Max.Y
	}
	for y := y1; y < y2; y++ {
		img.SetRGBA(x, y, stroke)
	}
}

func drawCenteredText(img *image.RGBA, x1, x2, y int, text string, col color.RGBA, scale int) {
	width := textWidth(text, scale)
	x := x1 + (x2-x1-width)/2
	if x < x1+2 {
		x = x1 + 2
	}
	drawText(img, x, y, text, col, scale)
}

func drawText(img *image.RGBA, x, y int, text string, col color.RGBA, scale int) {
	if scale <= 0 {
		scale = 1
	}
	cursor := x
	for _, r := range strings.ToUpper(text) {
		if r == ' ' {
			cursor += 4 * scale
			continue
		}
		pattern, ok := bitmapFont[r]
		if !ok {
			pattern = bitmapFont['?']
		}
		for row, line := range pattern {
			for colIndex, px := range line {
				if px != '1' {
					continue
				}
				fillRect(img, image.Rect(
					cursor+colIndex*scale,
					y+row*scale,
					cursor+(colIndex+1)*scale,
					y+(row+1)*scale,
				), col)
			}
		}
		cursor += 6 * scale
	}
}

func textWidth(text string, scale int) int {
	if text == "" {
		return 0
	}
	width := 0
	for _, r := range text {
		if r == ' ' {
			width += 4 * scale
		} else {
			width += 6 * scale
		}
	}
	return width - scale
}

var bitmapFont = map[rune][]string{
	'0': {"111", "101", "101", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "010", "010", "111"},
	'2': {"111", "001", "001", "111", "100", "100", "111"},
	'3': {"111", "001", "001", "111", "001", "001", "111"},
	'4': {"101", "101", "101", "111", "001", "001", "001"},
	'5': {"111", "100", "100", "111", "001", "001", "111"},
	'6': {"111", "100", "100", "111", "101", "101", "111"},
	'7': {"111", "001", "001", "010", "010", "100", "100"},
	'8': {"111", "101", "101", "111", "101", "101", "111"},
	'9': {"111", "101", "101", "111", "001", "001", "111"},
	'A': {"010", "101", "101", "111", "101", "101", "101"},
	'B': {"110", "101", "101", "110", "101", "101", "110"},
	'C': {"111", "100", "100", "100", "100", "100", "111"},
	'D': {"110", "101", "101", "101", "101", "101", "110"},
	'E': {"111", "100", "100", "111", "100", "100", "111"},
	'F': {"111", "100", "100", "111", "100", "100", "100"},
	'G': {"111", "100", "100", "101", "101", "101", "111"},
	'H': {"101", "101", "101", "111", "101", "101", "101"},
	'I': {"111", "010", "010", "010", "010", "010", "111"},
	'J': {"001", "001", "001", "001", "101", "101", "111"},
	'K': {"101", "101", "110", "100", "110", "101", "101"},
	'L': {"100", "100", "100", "100", "100", "100", "111"},
	'M': {"101", "111", "111", "101", "101", "101", "101"},
	'N': {"101", "111", "111", "111", "111", "111", "101"},
	'O': {"111", "101", "101", "101", "101", "101", "111"},
	'P': {"111", "101", "101", "111", "100", "100", "100"},
	'Q': {"111", "101", "101", "101", "101", "111", "001"},
	'R': {"110", "101", "101", "110", "101", "101", "101"},
	'S': {"111", "100", "100", "111", "001", "001", "111"},
	'T': {"111", "010", "010", "010", "010", "010", "010"},
	'U': {"101", "101", "101", "101", "101", "101", "111"},
	'V': {"101", "101", "101", "101", "101", "101", "010"},
	'W': {"101", "101", "101", "101", "111", "111", "101"},
	'X': {"101", "101", "101", "010", "101", "101", "101"},
	'Y': {"101", "101", "101", "010", "010", "010", "010"},
	'Z': {"111", "001", "001", "010", "100", "100", "111"},
	':': {"0", "1", "0", "0", "1", "0", "0"},
	'.': {"0", "0", "0", "0", "0", "0", "1"},
	'-': {"0", "0", "0", "1", "0", "0", "0"},
	'/': {"001", "001", "010", "010", "100", "100", "100"},
	'@': {"111", "101", "111", "111", "100", "100", "111"},
	'+': {"0", "1", "1", "1", "1", "1", "0"},
	'#': {"101", "111", "101", "101", "111", "101", "101"},
	'?': {"111", "001", "001", "011", "010", "000", "010"},
	'Б': {"111", "100", "100", "111", "101", "101", "111"},
	'В': {"110", "101", "101", "110", "101", "101", "110"},
	'Н': {"101", "101", "101", "111", "101", "101", "101"},
	'П': {"111", "101", "101", "101", "101", "101", "101"},
	'Р': {"111", "101", "101", "111", "100", "100", "100"},
	'С': {"111", "100", "100", "100", "100", "100", "111"},
	'Т': {"111", "010", "010", "010", "010", "010", "010"},
	'Ч': {"101", "101", "101", "011", "001", "001", "001"},
}
