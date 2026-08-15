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
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	scheduleImageWidth  = 1600
	schedulePanelHeight = 1060
)

var (
	scheduleBg        = color.RGBA{R: 238, G: 243, B: 241, A: 255}
	schedulePanel     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	scheduleGrid      = color.RGBA{R: 207, G: 216, B: 218, A: 255}
	scheduleGridSoft  = color.RGBA{R: 232, G: 237, B: 238, A: 255}
	scheduleText      = color.RGBA{R: 31, G: 41, B: 45, A: 255}
	scheduleMutedText = color.RGBA{R: 101, G: 115, B: 121, A: 255}
	scheduleFree      = color.RGBA{R: 224, G: 242, B: 230, A: 255}
	scheduleClosed    = color.RGBA{R: 225, G: 229, B: 231, A: 255}
	scheduleBlocked   = color.RGBA{R: 167, G: 176, B: 181, A: 255}
	scheduleToday     = color.RGBA{R: 226, G: 239, B: 243, A: 255}
	scheduleWhite     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	bookingPalette    = []color.RGBA{
		{R: 246, G: 191, B: 176, A: 255},
		{R: 164, G: 207, B: 220, A: 255},
		{R: 188, G: 216, B: 174, A: 255},
		{R: 242, G: 213, B: 151, A: 255},
		{R: 208, G: 190, B: 224, A: 255},
	}
)

type scheduleSlotGroup struct {
	Name     string
	Slots    []ScheduleGridSlot
	Bookings []BookingView
}

type scheduleFontSet struct {
	title font.Face
	day   font.Face
	time  font.Face
	body  font.Face
	small font.Face
}

func renderScheduleWeekImage(lang string, start time.Time, slots []ScheduleGridSlot, bookings []BookingView) ([]byte, error) {
	start = weekStart(start)
	groups := groupScheduleSlots(slots, bookings)
	height := 48 + len(groups)*schedulePanelHeight + 24
	img := image.NewRGBA(image.Rect(0, 0, scheduleImageWidth, height))
	fillRect(img, img.Bounds(), scheduleBg)

	fonts, err := newScheduleFontSet()
	if err != nil {
		return nil, err
	}
	defer fonts.close()

	y := 24
	for _, group := range groups {
		drawScheduleWeekPanel(img, fonts, lang, start, group, y)
		y += schedulePanelHeight
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newScheduleFontSet() (scheduleFontSet, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return scheduleFontSet{}, fmt.Errorf("parse schedule regular font: %w", err)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return scheduleFontSet{}, fmt.Errorf("parse schedule bold font: %w", err)
	}
	newFace := func(parsed *opentype.Font, size float64) (font.Face, error) {
		return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 96, Hinting: font.HintingFull})
	}
	faces := scheduleFontSet{}
	if faces.title, err = newFace(bold, 28); err != nil {
		return scheduleFontSet{}, err
	}
	if faces.day, err = newFace(bold, 18); err != nil {
		faces.close()
		return scheduleFontSet{}, err
	}
	if faces.time, err = newFace(bold, 14); err != nil {
		faces.close()
		return scheduleFontSet{}, err
	}
	if faces.body, err = newFace(regular, 14); err != nil {
		faces.close()
		return scheduleFontSet{}, err
	}
	if faces.small, err = newFace(regular, 12); err != nil {
		faces.close()
		return scheduleFontSet{}, err
	}
	return faces, nil
}

func (f scheduleFontSet) close() {
	for _, face := range []font.Face{f.title, f.day, f.time, f.body, f.small} {
		if closer, ok := face.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

func groupScheduleSlots(slots []ScheduleGridSlot, bookings []BookingView) []scheduleSlotGroup {
	hasNames := false
	for _, slot := range slots {
		if strings.TrimSpace(slot.AdminName) != "" {
			hasNames = true
			break
		}
	}
	if !hasNames {
		for _, booking := range bookings {
			if strings.TrimSpace(booking.AdminName) != "" {
				hasNames = true
				break
			}
		}
	}
	if !hasNames {
		return []scheduleSlotGroup{{Slots: sortedScheduleSlots(slots), Bookings: sortedScheduleBookings(bookings)}}
	}

	groups := make(map[string]*scheduleSlotGroup)
	for _, slot := range slots {
		name := scheduleAdminName(slot.AdminName)
		if groups[name] == nil {
			groups[name] = &scheduleSlotGroup{Name: name}
		}
		groups[name].Slots = append(groups[name].Slots, slot)
	}
	for _, booking := range bookings {
		name := scheduleAdminName(booking.AdminName)
		if groups[name] == nil {
			groups[name] = &scheduleSlotGroup{Name: name}
		}
		groups[name].Bookings = append(groups[name].Bookings, booking)
	}

	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]scheduleSlotGroup, 0, len(names))
	for _, name := range names {
		group := groups[name]
		group.Slots = sortedScheduleSlots(group.Slots)
		group.Bookings = sortedScheduleBookings(group.Bookings)
		out = append(out, *group)
	}
	return out
}

func scheduleAdminName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "admin"
	}
	return value
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

func sortedScheduleBookings(bookings []BookingView) []BookingView {
	out := append([]BookingView(nil), bookings...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartAt.Equal(out[j].StartAt) {
			return out[i].StartAt.Before(out[j].StartAt)
		}
		return out[i].AdminName < out[j].AdminName
	})
	return out
}

func drawScheduleWeekPanel(img *image.RGBA, fonts scheduleFontSet, lang string, week time.Time, group scheduleSlotGroup, top int) {
	left := 28
	right := scheduleImageWidth - 28
	bottom := top + schedulePanelHeight - 20
	panelRect := image.Rect(left, top, right, bottom)
	fillRect(img, panelRect, schedulePanel)
	strokeRect(img, panelRect, scheduleGrid)

	title := scheduleWeekTitle(lang, week)
	if group.Name != "" {
		title += "  @" + group.Name
	}
	drawFontText(img, fonts.title, left+32, top+24, fitFontText(title, right-left-64, fonts.title), scheduleText)

	minMinute, maxMinute := scheduleMinuteRange(group.Slots, group.Bookings)
	timeLeft := left + 20
	gridLeft := left + 104
	gridRight := right - 20
	headerTop := top + 82
	headerHeight := 62
	gridTop := headerTop + headerHeight
	gridBottom := bottom - 82
	gridHeight := gridBottom - gridTop
	colWidth := (gridRight - gridLeft) / 7

	for i := 0; i < 7; i++ {
		x1, x2 := scheduleDayColumn(gridLeft, gridRight, colWidth, i)
		day := week.AddDate(0, 0, i)
		fill := scheduleWhite
		if sameCalendarDay(day, time.Now().In(day.Location())) {
			fill = scheduleToday
		}
		fillRect(img, image.Rect(x1, headerTop, x2, gridBottom), fill)
		strokeRect(img, image.Rect(x1, headerTop, x2, gridBottom), scheduleGrid)
		label := weekdayShort(lang, day.Weekday()) + "  " + day.Format("02.01")
		drawCenteredFontText(img, fonts.day, x1, x2, headerTop+18, label, scheduleText)
	}

	for _, slot := range group.Slots {
		dayIndex := localDayIndex(week, slot.StartAt)
		if dayIndex < 0 || dayIndex >= 7 {
			continue
		}
		x1, x2 := scheduleDayColumn(gridLeft, gridRight, colWidth, dayIndex)
		y1, y2 := scheduleIntervalY(slot.StartAt, slot.EndAt, minMinute, maxMinute, gridTop, gridHeight)
		fillRect(img, image.Rect(x1+1, y1, x2-1, y2), scheduleSlotBackground(slot))
	}

	for minute := minMinute; minute <= maxMinute; minute += 30 {
		y := minuteToY(minute, minMinute, maxMinute, gridTop, gridHeight)
		lineColor := scheduleGridSoft
		if minute%60 == 0 {
			lineColor = scheduleGrid
			drawFontText(img, fonts.small, timeLeft, y-8, formatMinuteLabel(minute), scheduleMutedText)
		}
		drawLineH(img, gridLeft, gridRight, y, lineColor)
	}

	if len(group.Slots) == 0 && len(group.Bookings) == 0 {
		message := "Расписание не создано"
		if lang == LangEN {
			message = "No schedule"
		}
		drawCenteredFontText(img, fonts.day, gridLeft, gridRight, gridTop+gridHeight/2-12, message, scheduleMutedText)
	}

	for _, booking := range group.Bookings {
		drawScheduleBooking(img, fonts, week, booking, minMinute, maxMinute, gridLeft, gridRight, gridTop, gridHeight, colWidth)
	}

	legendY := bottom - 52
	drawScheduleLegend(img, fonts, left+34, legendY, lang)
}

func drawScheduleBooking(img *image.RGBA, fonts scheduleFontSet, week time.Time, booking BookingView, minMinute, maxMinute, gridLeft, gridRight, gridTop, gridHeight, colWidth int) {
	dayIndex := localDayIndex(week, booking.StartAt)
	if dayIndex < 0 || dayIndex >= 7 {
		return
	}
	x1, x2 := scheduleDayColumn(gridLeft, gridRight, colWidth, dayIndex)
	y1, y2 := scheduleIntervalY(booking.StartAt, booking.EndAt, minMinute, maxMinute, gridTop, gridHeight)
	x1 += 5
	x2 -= 5
	y1 += 2
	y2 -= 2
	if y2-y1 < 24 {
		y2 = y1 + 24
	}
	if y2 > gridTop+gridHeight {
		y2 = gridTop + gridHeight
	}
	rect := image.Rect(x1, y1, x2, y2)
	fillRect(img, rect, scheduleBookingColor(booking))
	strokeRect(img, rect, color.RGBA{R: 116, G: 128, B: 132, A: 255})

	padding := 7
	availableWidth := x2 - x1 - padding*2
	height := y2 - y1
	timeLabel := booking.StartAt.Format("15:04") + "-" + booking.EndAt.Format("15:04")
	client := formatClientContact(booking.Username)
	service := strings.Join(booking.ServiceNames, ", ")
	if service == "" {
		service = "Запись"
	}
	if height < 42 {
		line := booking.StartAt.Format("15:04")
		if client != "" {
			line += " " + client
		}
		drawFontText(img, fonts.small, x1+padding, y1+1, fitFontText(line, availableWidth, fonts.small), scheduleText)
		if height >= 34 {
			drawFontText(img, fonts.small, x1+padding, y1+17, fitFontText(service, availableWidth, fonts.small), scheduleText)
		}
		return
	}
	drawFontText(img, fonts.time, x1+padding, y1+5, fitFontText(timeLabel, availableWidth, fonts.time), scheduleText)
	secondLine := client
	if height < 68 && client != "" {
		secondLine = client + " | " + service
	} else if secondLine == "" {
		secondLine = service
	}
	drawFontText(img, fonts.small, x1+padding, y1+24, fitFontText(secondLine, availableWidth, fonts.small), scheduleText)
	if height >= 68 && client != "" {
		drawFontText(img, fonts.small, x1+padding, y1+43, fitFontText(service, availableWidth, fonts.small), scheduleText)
	}
}

func scheduleDayColumn(gridLeft, gridRight, colWidth, dayIndex int) (int, int) {
	x1 := gridLeft + dayIndex*colWidth
	x2 := x1 + colWidth
	if dayIndex == 6 {
		x2 = gridRight
	}
	return x1, x2
}

func scheduleIntervalY(start, end time.Time, minMinute, maxMinute, gridTop, gridHeight int) (int, int) {
	startMinute := minuteOfDay(start)
	endMinute := minuteOfDay(end)
	if endMinute <= startMinute {
		endMinute = startMinute + 30
	}
	y1 := minuteToY(startMinute, minMinute, maxMinute, gridTop, gridHeight)
	y2 := minuteToY(endMinute, minMinute, maxMinute, gridTop, gridHeight)
	if y2 <= y1 {
		y2 = y1 + 1
	}
	return y1, y2
}

func scheduleMinuteRange(slots []ScheduleGridSlot, bookings []BookingView) (int, int) {
	minMinute := 9 * 60
	maxMinute := 18 * 60
	if len(slots) > 0 || len(bookings) > 0 {
		minMinute = 24 * 60
		maxMinute = 0
		for _, slot := range slots {
			minMinute, maxMinute = expandScheduleMinuteRange(minMinute, maxMinute, slot.StartAt, slot.EndAt)
		}
		for _, booking := range bookings {
			minMinute, maxMinute = expandScheduleMinuteRange(minMinute, maxMinute, booking.StartAt, booking.EndAt)
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

func expandScheduleMinuteRange(minMinute, maxMinute int, start, end time.Time) (int, int) {
	startMinute := minuteOfDay(start)
	endMinute := minuteOfDay(end)
	if endMinute <= startMinute {
		endMinute = startMinute + 30
	}
	if startMinute < minMinute {
		minMinute = startMinute
	}
	if endMinute > maxMinute {
		maxMinute = endMinute
	}
	return minMinute, maxMinute
}

func scheduleSlotBackground(slot ScheduleGridSlot) color.RGBA {
	if strings.ToLower(slot.Status) != "open" {
		return scheduleClosed
	}
	if slot.Blocked > 0 {
		return scheduleBlocked
	}
	return scheduleFree
}

func scheduleBookingColor(booking BookingView) color.RGBA {
	key := booking.Username + strings.Join(booking.ServiceNames, "|")
	hash := 0
	for _, r := range key {
		hash = (hash*31 + int(r)) & 0x7fffffff
	}
	return bookingPalette[hash%len(bookingPalette)]
}

func drawScheduleLegend(img *image.RGBA, fonts scheduleFontSet, x, y int, lang string) {
	items := []struct {
		label string
		fill  color.RGBA
	}{
		{label: tr(lang, "week_legend_free"), fill: scheduleFree},
		{label: tr(lang, "week_legend_booking"), fill: bookingPalette[0]},
		{label: tr(lang, "week_legend_closed"), fill: scheduleClosed},
	}
	for _, item := range items {
		fillRect(img, image.Rect(x, y, x+24, y+24), item.fill)
		strokeRect(img, image.Rect(x, y, x+24, y+24), scheduleGrid)
		drawFontText(img, fonts.body, x+34, y+2, item.label, scheduleMutedText)
		x += 230
	}
}

func scheduleWeekTitle(lang string, start time.Time) string {
	if lang == LangEN {
		return fmt.Sprintf("Schedule | %s-%s %d", start.Format("02 Jan"), start.AddDate(0, 0, 6).Format("02 Jan"), start.Year())
	}
	return fmt.Sprintf("График | %s-%s %d", start.Format("02.01"), start.AddDate(0, 0, 6).Format("02.01"), start.Year())
}

func scheduleWeekCaption(lang string, start time.Time) string {
	return tr(lang, "week_caption", start.Format("02.01"), start.AddDate(0, 0, 6).Format("02.01"))
}

func sameCalendarDay(left, right time.Time) bool {
	return left.Year() == right.Year() && left.YearDay() == right.YearDay()
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

func drawFontText(img *image.RGBA, face font.Face, x, y int, text string, col color.RGBA) {
	if face == nil || text == "" {
		return
	}
	drawer := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y+face.Metrics().Ascent.Ceil()),
	}
	drawer.DrawString(text)
}

func drawCenteredFontText(img *image.RGBA, face font.Face, x1, x2, y int, text string, col color.RGBA) {
	width := font.MeasureString(face, text).Ceil()
	x := x1 + (x2-x1-width)/2
	if x < x1+3 {
		x = x1 + 3
	}
	drawFontText(img, face, x, y, fitFontText(text, x2-x-3, face), col)
}

func fitFontText(value string, maxWidth int, face font.Face) string {
	value = strings.TrimSpace(value)
	if value == "" || maxWidth <= 0 || font.MeasureString(face, value).Ceil() <= maxWidth {
		return value
	}
	suffix := "..."
	for utf8.RuneCountInString(value) > 1 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = strings.TrimSpace(value[:len(value)-size])
		if font.MeasureString(face, value+suffix).Ceil() <= maxWidth {
			return value + suffix
		}
	}
	return suffix
}

func fillRect(img *image.RGBA, rect image.Rectangle, fill color.RGBA) {
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return
	}
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
	x1 = max(x1, img.Bounds().Min.X)
	x2 = minInt(x2, img.Bounds().Max.X)
	for x := x1; x < x2; x++ {
		img.SetRGBA(x, y, stroke)
	}
}

func drawLineV(img *image.RGBA, x, y1, y2 int, stroke color.RGBA) {
	if x < img.Bounds().Min.X || x >= img.Bounds().Max.X {
		return
	}
	y1 = max(y1, img.Bounds().Min.Y)
	y2 = minInt(y2, img.Bounds().Max.Y)
	for y := y1; y < y2; y++ {
		img.SetRGBA(x, y, stroke)
	}
}
