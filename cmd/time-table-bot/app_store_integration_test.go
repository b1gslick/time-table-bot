//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image/png"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/domain"
	"time-table-bot/internal/nlu"
	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

var appStoreIntegrationDB *sql.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, db, err := startAppStorePostgresContainer(ctx)
	if err != nil {
		log.Printf("start postgres container: %v", err)
		os.Exit(1)
	}
	appStoreIntegrationDB = db

	if err := store.NewPostgresStore(db).ApplySchema(ctx); err != nil {
		log.Printf("apply schema: %v", err)
		_ = db.Close()
		if container != nil {
			_ = container.Terminate(context.Background())
		}
		os.Exit(1)
	}

	code := m.Run()
	_ = db.Close()
	if container != nil {
		_ = container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestAppStore_ServiceDurationAvailabilityFlow(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)

	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := app.RegisterOrUpdateUser(ctx, bot.UserRecord{TelegramID: 3001, Username: "client", Language: bot.LangRU}); err != nil {
		t.Fatalf("Register client: %v", err)
	}

	if err := app.AddService(ctx, 2001, "Nails > Manicure > service 1", 30, ""); err != nil {
		t.Fatalf("AddService 1: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Nails > Manicure > service 4", 45, ""); err != nil {
		t.Fatalf("AddService 4: %v", err)
	}

	future := time.Now().In(loc).AddDate(0, 2, 0)
	start := time.Date(future.Year(), future.Month(), 10, 10, 0, 0, 0, loc)
	for i := 0; i < 8; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}

	services, err := app.ListServices(ctx, 3001)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %d, want 2", len(services))
	}
	if services[0].Category != "Nails" || services[0].Subcategory != "Manicure" || services[0].Name != "service 1" {
		t.Fatalf("service path = %q > %q > %q, want Nails > Manicure > service 1", services[0].Category, services[0].Subcategory, services[0].Name)
	}

	freeForDuplicate, err := app.ListFreeSlotsForServicesDates(ctx, 3001, []int{1, 1}, []time.Time{start})
	if err != nil {
		t.Fatalf("ListFreeSlotsForServicesDates duplicate service: %v", err)
	}
	if len(freeForDuplicate) == 0 {
		t.Fatal("duplicate service selection should still return slots for a single selected service")
	}
	if freeForDuplicate[0].DurationMin != 30 {
		t.Fatalf("duplicate service duration = %d, want 30", freeForDuplicate[0].DurationMin)
	}

	free, err := app.ListFreeSlotsForServices(ctx, 3001, []int{1, 2}, start)
	if err != nil {
		t.Fatalf("ListFreeSlotsForServices: %v", err)
	}
	if len(free) != 4 {
		t.Fatalf("free slots = %d, want 4", len(free))
	}
	if free[0].DurationMin != 75 || !free[0].EndAt.Equal(start.Add(75*time.Minute)) {
		t.Fatalf("first free slot = %#v, want 75 minute interval", free[0])
	}

	bookingResult, err := app.BookForUserByIndex(ctx, 3001, 1)
	if err != nil {
		t.Fatalf("BookForUserByIndex: %v", err)
	}
	if !bookingResult.StartAt.Equal(start) {
		t.Fatalf("booked start = %s, want %s", bookingResult.StartAt, start)
	}

	free, err = app.ListFreeSlotsForServices(ctx, 3001, []int{1, 2}, start)
	if err != nil {
		t.Fatalf("ListFreeSlotsForServices after booking: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("free slots after booking = %d, want 0", len(free))
	}
}

func TestAppStore_AllowsSameServiceWithDifferentDurations(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 30, ""); err != nil {
		t.Fatalf("AddService 30: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 45, ""); err != nil {
		t.Fatalf("AddService 45: %v", err)
	}

	services, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %#v, want two duration variants", services)
	}
	if services[0].Name != "Classic" || services[0].DurationMin != 30 || services[1].Name != "Classic" || services[1].DurationMin != 45 {
		t.Fatalf("services = %#v, want same service name with 30 and 45 min", services)
	}
}

func TestAppStore_CategoryOrderChangesServiceListNumbers(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 30, ""); err != nil {
		t.Fatalf("AddService Nails: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Hair > Cut > Short", 45, ""); err != nil {
		t.Fatalf("AddService Hair: %v", err)
	}
	if err := app.SetCategoryOrder(ctx, 2001, []string{"Hair", "Nails"}); err != nil {
		t.Fatalf("SetCategoryOrder: %v", err)
	}

	services, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 2 || services[0].Category != "Hair" || services[1].Category != "Nails" {
		t.Fatalf("services = %#v, want Hair category first", services)
	}

	future := time.Now().UTC().AddDate(0, 2, 0)
	start := time.Date(future.Year(), future.Month(), 10, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}
	slots, err := app.ListFreeSlotsForServicesRange(ctx, 2001, []int{1}, start.Add(-time.Hour), start.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListFreeSlotsForServicesRange: %v", err)
	}
	if len(slots) != 1 || len(slots[0].ServiceNames) != 1 || slots[0].ServiceNames[0] != "Short" {
		t.Fatalf("slots = %#v, want first visible service Hair > Short", slots)
	}
}

func TestAppStore_SuperAdminSeesAdminServicesCalendarsAndBookings(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)

	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	admin1, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin1: %v", err)
	}
	admin2, err := repo.UpsertUser(ctx, 2002, "second", "Second")
	if err != nil {
		t.Fatalf("UpsertUser admin2: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id IN ($2, $3)", domain.RoleAdmin, admin1.ID, admin2.ID); err != nil {
		t.Fatalf("promote admins: %v", err)
	}
	client, err := repo.UpsertUser(ctx, 3001, "client", "Client")
	if err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}

	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 30, ""); err != nil {
		t.Fatalf("AddService admin1: %v", err)
	}
	if err := app.AddService(ctx, 2002, "Hair > Cut > Short", 45, ""); err != nil {
		t.Fatalf("AddService admin2: %v", err)
	}
	adminServices, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices admin: %v", err)
	}
	if len(adminServices) != 1 || adminServices[0].AdminName != "master" {
		t.Fatalf("admin services = %#v, want only master service", adminServices)
	}
	superServices, err := app.ListServices(ctx, 1001)
	if err != nil {
		t.Fatalf("ListServices super: %v", err)
	}
	if len(superServices) != 2 {
		t.Fatalf("super services = %#v, want both admin services", superServices)
	}

	start1 := time.Date(2026, 6, 1, 10, 0, 0, 0, loc)
	start2 := time.Date(2026, 6, 2, 11, 0, 0, 0, loc)
	slot1, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
		AdminUserID: admin1.ID,
		StartAt:     start1,
		EndAt:       start1.Add(30 * time.Minute),
		Capacity:    1,
		Status:      domain.SlotStatusOpen,
	})
	if err != nil {
		t.Fatalf("CreateScheduleSlot admin1: %v", err)
	}
	slot2, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
		AdminUserID: admin2.ID,
		StartAt:     start2,
		EndAt:       start2.Add(45 * time.Minute),
		Capacity:    1,
		Status:      domain.SlotStatusOpen,
	})
	if err != nil {
		t.Fatalf("CreateScheduleSlot admin2: %v", err)
	}
	clientID := client.ID
	if _, err := repo.CreateBooking(ctx, domain.Booking{SlotID: slot1.ID, UserID: &clientID, Status: domain.BookingStatusBooked, Note: "created_by_user"}); err != nil {
		t.Fatalf("CreateBooking admin1: %v", err)
	}
	if _, err := repo.CreateBooking(ctx, domain.Booking{SlotID: slot2.ID, UserID: &clientID, Status: domain.BookingStatusBooked, Note: "created_by_user"}); err != nil {
		t.Fatalf("CreateBooking admin2: %v", err)
	}

	adminBookings, err := app.ListAdminBookings(ctx, 2001, start1.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListAdminBookings admin: %v", err)
	}
	if len(adminBookings) != 1 || adminBookings[0].Username != "client" || adminBookings[0].AdminName != "" {
		t.Fatalf("admin bookings = %#v, want one client booking without admin label", adminBookings)
	}
	superBookings, err := app.ListAdminBookings(ctx, 1001, start1.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListAdminBookings super: %v", err)
	}
	if len(superBookings) != 2 || superBookings[0].AdminName == "" || superBookings[1].AdminName == "" {
		t.Fatalf("super bookings = %#v, want bookings with admin labels", superBookings)
	}

	superCalendar, err := app.AdminCalendar(ctx, 1001, time.Date(2026, 6, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatalf("AdminCalendar super: %v", err)
	}
	seen := map[string]bool{}
	for _, day := range superCalendar {
		seen[day.AdminName] = true
	}
	if !seen["master"] || !seen["second"] || seen[super.Username] {
		t.Fatalf("super calendar admin names = %#v, want master and second", seen)
	}
}

func TestAppStore_AddBookingByPhoneUsesSuperAdminView(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)

	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleSuperAdmin, super.ID); err != nil {
		t.Fatalf("promote super: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleAdmin, AdminUsername: "master"}); err != nil {
		t.Fatalf("SetSuperAdminView: %v", err)
	}

	start := time.Date(2026, 7, 7, 12, 0, 0, 0, loc)
	if _, err := app.AddBookingByPhone(ctx, 1001, "+357 99 999999", start); err != nil {
		t.Fatalf("AddBookingByPhone: %v", err)
	}

	bookings, err := app.ListAdminBookings(ctx, 2001, start.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListAdminBookings admin: %v", err)
	}
	if len(bookings) != 1 {
		t.Fatalf("bookings = %#v, want one booking", bookings)
	}
	if bookings[0].Username != "+35799999999" || !bookings[0].StartAt.Equal(start) {
		t.Fatalf("booking = %#v, want phone client at %s", bookings[0], start)
	}

	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleSuperAdmin}); err != nil {
		t.Fatalf("reset super view: %v", err)
	}
	result, err := app.DeleteBookingByID(ctx, 1001, bookings[0].ID)
	if err != nil {
		t.Fatalf("DeleteBookingByID as super admin: %v", err)
	}
	if result.AdminChatID != 2001 || result.Username != "+35799999999" {
		t.Fatalf("delete result = %#v, want target admin chat and phone client", result)
	}
	var cancelled int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE id = $1 AND status = 'cancelled'", bookings[0].ID).Scan(&cancelled); err != nil {
		t.Fatalf("count cancelled booking: %v", err)
	}
	if cancelled != 1 {
		t.Fatalf("cancelled booking count = %d, want 1", cancelled)
	}
}

func TestAppStore_GenerateScheduleForMultipleMonths(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "work_days", "mon"); err != nil {
		t.Fatalf("set work_days: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "work_start", "10:00"); err != nil {
		t.Fatalf("set work_start: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "work_end", "11:00"); err != nil {
		t.Fatalf("set work_end: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "session_duration", "30"); err != nil {
		t.Fatalf("set session_duration: %v", err)
	}

	pastMonth := time.Date(2026, 6, 1, 0, 0, 0, 0, loc)
	futureMonth := time.Now().In(loc).AddDate(0, 2, 0)
	futureMonth = time.Date(futureMonth.Year(), futureMonth.Month(), 1, 0, 0, 0, 0, loc)
	nextFutureMonth := futureMonth.AddDate(0, 1, 0)

	if _, err := app.GenerateSchedule(ctx, 2001, bot.GenerateScheduleRequest{
		Month:  pastMonth,
		Months: 1,
	}); err != nil {
		t.Fatalf("GenerateSchedule past month: %v", err)
	}
	result, err := app.GenerateSchedule(ctx, 2001, bot.GenerateScheduleRequest{
		Month:  futureMonth,
		Months: 2,
	})
	if err != nil {
		t.Fatalf("GenerateSchedule: %v", err)
	}
	if result.Created == 0 {
		t.Fatalf("created slots = 0, want slots for two months")
	}

	var futureSlots, nextFutureSlots int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3
`, admin.ID, futureMonth, nextFutureMonth).Scan(&futureSlots); err != nil {
		t.Fatalf("count future month slots: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3
`, admin.ID, nextFutureMonth, nextFutureMonth.AddDate(0, 1, 0)).Scan(&nextFutureSlots); err != nil {
		t.Fatalf("count next future month slots: %v", err)
	}
	if futureSlots == 0 || nextFutureSlots == 0 {
		t.Fatalf("futureSlots=%d nextFutureSlots=%d, want both future months filled", futureSlots, nextFutureSlots)
	}
	months, err := app.ListScheduleMonths(ctx, 2001)
	if err != nil {
		t.Fatalf("ListScheduleMonths: %v", err)
	}
	if len(months) != 2 || !months[0].Month.Equal(futureMonth) || !months[1].Month.Equal(nextFutureMonth) {
		t.Fatalf("months = %#v, want %s and %s", months, futureMonth.Format("2006-01"), nextFutureMonth.Format("2006-01"))
	}
	pastDays, err := app.ListScheduleDays(ctx, 2001, pastMonth)
	if err != nil {
		t.Fatalf("ListScheduleDays past: %v", err)
	}
	if len(pastDays) != 0 {
		t.Fatalf("past days = %#v, want none", pastDays)
	}
	days, err := app.ListScheduleDays(ctx, 2001, futureMonth)
	if err != nil {
		t.Fatalf("ListScheduleDays: %v", err)
	}
	if len(days) == 0 || days[0].Date.Before(dateOnlyLocal(time.Now().In(loc), loc)) {
		t.Fatalf("days = %#v, want future generated days", days)
	}
	weekdays, err := app.ListScheduleWeekdays(ctx, 2001, futureMonth)
	if err != nil {
		t.Fatalf("ListScheduleWeekdays: %v", err)
	}
	if len(weekdays) != 1 || weekdays[0].Weekday != time.Monday {
		t.Fatalf("weekdays = %#v, want Monday only", weekdays)
	}
}

func TestAppStore_GenerateScheduleForSpecificDateClearsAvailabilityCache(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	client, err := repo.UpsertUser(ctx, 3001, "client", "Client")
	if err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "session_duration", "30"); err != nil {
		t.Fatalf("set session_duration: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, client.ID, "last_availability_slots", `[{"stale":true}]`); err != nil {
		t.Fatalf("seed client availability cache: %v", err)
	}

	day := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	result, err := app.GenerateSchedule(ctx, 2001, bot.GenerateScheduleRequest{
		Date:     day,
		DayStart: 10 * time.Hour,
		DayEnd:   11 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateSchedule date: %v", err)
	}
	if result.Created != 2 || result.Skipped != 0 {
		t.Fatalf("result = %+v, want 2 created and 0 skipped", result)
	}

	result, err = app.GenerateSchedule(ctx, 2001, bot.GenerateScheduleRequest{
		Date:        day,
		DayStart:    10 * time.Hour,
		DayEnd:      11 * time.Hour,
		DurationMin: 30,
	})
	if err != nil {
		t.Fatalf("GenerateSchedule date duplicate: %v", err)
	}
	if result.Created != 0 || result.Skipped != 2 {
		t.Fatalf("duplicate result = %+v, want 0 created and 2 skipped", result)
	}

	var slots, staleCache int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3
`, admin.ID, day, day.AddDate(0, 0, 1)).Scan(&slots); err != nil {
		t.Fatalf("count day slots: %v", err)
	}
	if slots != 2 {
		t.Fatalf("day slots = %d, want 2", slots)
	}
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM admin_settings
WHERE key IN ('last_free_slots', 'last_availability_slots') OR key LIKE 'last_move_availability:%'
`).Scan(&staleCache); err != nil {
		t.Fatalf("count stale cache: %v", err)
	}
	if staleCache != 0 {
		t.Fatalf("stale cache rows = %d, want 0", staleCache)
	}
}

func TestAppStore_ServiceChangesUseSuperAdminView(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	app := newAppStore(db, repo, time.UTC)
	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleSuperAdmin, super.ID); err != nil {
		t.Fatalf("promote super: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleAdmin, AdminUsername: "master"}); err != nil {
		t.Fatalf("SetSuperAdminView: %v", err)
	}

	if err := app.AddService(ctx, 1001, "Electro > 2 hours", 120, ""); err != nil {
		t.Fatalf("AddService through view: %v", err)
	}
	adminServices, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices admin: %v", err)
	}
	if len(adminServices) != 1 || adminServices[0].AdminName != "master" || adminServices[0].Name != "2 hours" {
		t.Fatalf("admin services = %#v, want service on target admin", adminServices)
	}

	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleSuperAdmin}); err != nil {
		t.Fatalf("reset super view: %v", err)
	}
	superServices, err := app.ListServices(ctx, 1001)
	if err != nil {
		t.Fatalf("ListServices super: %v", err)
	}
	for _, service := range superServices {
		if service.AdminName == "tim1106" && service.Name == "2 hours" {
			t.Fatalf("service was added to super admin instead of target admin: %#v", superServices)
		}
	}
}

func TestAppStore_ProfileTextUsesSuperAdminView(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	app := newAppStore(db, repo, time.UTC)
	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleSuperAdmin, super.ID); err != nil {
		t.Fatalf("promote super: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleAdmin, AdminUsername: "master"}); err != nil {
		t.Fatalf("SetSuperAdminView: %v", err)
	}

	if err := app.SetProfileText(ctx, 1001, "Target profile"); err != nil {
		t.Fatalf("SetProfileText through view: %v", err)
	}
	got, err := app.GetProfileText(ctx, 1001)
	if err != nil {
		t.Fatalf("GetProfileText through view: %v", err)
	}
	if got != "Target profile" {
		t.Fatalf("profile through view = %q, want Target profile", got)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "services_text", "legacy duplicate service description"); err != nil {
		t.Fatalf("seed legacy services text: %v", err)
	}
	intro, err := app.MasterIntro(ctx, 2001)
	if err != nil {
		t.Fatalf("MasterIntro: %v", err)
	}
	if !strings.Contains(intro, "Target profile") || strings.Contains(intro, "legacy duplicate") {
		t.Fatalf("master intro includes legacy services description: %q", intro)
	}
	var superProfiles int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_profiles WHERE user_id = $1", super.ID).Scan(&superProfiles); err != nil {
		t.Fatalf("count super profiles: %v", err)
	}
	if superProfiles != 0 {
		t.Fatalf("super profile rows = %d, want 0", superProfiles)
	}
}

func TestAppStore_AddBookingForContactByIndexUsesSelectedServiceSlot(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 30, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 2; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}

	services, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("services = %#v, want one service", services)
	}
	slots, err := app.ListFreeSlotsForServicesRange(ctx, 2001, []int{1}, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListFreeSlotsForServicesRange: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("slots = %#v, want one 30-minute slot", slots)
	}
	result, err := app.AddBookingForContactByIndex(ctx, 2001, "phone", "+357 99 999999", 1)
	if err != nil {
		t.Fatalf("AddBookingForContactByIndex: %v", err)
	}
	if result.Username != "+35799999999" || len(result.ServiceNames) != 1 || result.ServiceNames[0] != "Classic" {
		t.Fatalf("booking result = %#v, want phone client and Classic service", result)
	}
}

func TestAppStore_FinanceReportIncludesBookingsExpensesAndOverrides(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	client, err := repo.UpsertUser(ctx, 3001, "client", "Client")
	if err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Электроэпиляция > Основное > 1 час 45 €", 60, ""); err != nil {
		t.Fatalf("AddService priced: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Восковая депиляция > Лицо > Усы", 30, ""); err != nil {
		t.Fatalf("AddService without price: %v", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT id FROM admin_services WHERE admin_user_id = $1 ORDER BY id", admin.ID)
	if err != nil {
		t.Fatalf("query services: %v", err)
	}
	var serviceIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan service: %v", err)
		}
		serviceIDs = append(serviceIDs, id)
	}
	rows.Close()
	if len(serviceIDs) != 2 {
		t.Fatalf("service ids = %v, want two", serviceIDs)
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i, serviceID := range serviceIDs {
		start := from.AddDate(0, 0, 2+i).Add(10 * time.Hour)
		slot, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			ServiceID:   &serviceID,
			StartAt:     start,
			EndAt:       start.Add(time.Hour),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		})
		if err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
		if _, err := repo.CreateBooking(ctx, domain.Booking{
			SlotID: slot.ID, UserID: &client.ID, ServiceID: &serviceID,
			Status: domain.BookingStatusBooked,
		}); err != nil {
			t.Fatalf("CreateBooking %d: %v", i, err)
		}
	}
	if err := app.AddFinanceEntry(ctx, 2001, bot.FinanceEntryInput{
		Kind: "expense", Category: "supplies", AmountCents: 1200, Currency: "EUR",
		OccurredAt: from.AddDate(0, 0, 4).Add(12 * time.Hour), Description: "Расходники", Source: "text",
	}); err != nil {
		t.Fatalf("AddFinanceEntry expense: %v", err)
	}

	report, err := app.FinanceReport(ctx, 2001, from, from.AddDate(0, 1, 0), "month")
	if err != nil {
		t.Fatalf("FinanceReport: %v", err)
	}
	if report.BookingIncomeCents != 4500 || report.ExpenseCents != 1200 || len(report.Unresolved) != 1 {
		t.Fatalf("report = %#v, want 45 EUR income, 12 EUR expense, one unresolved", report)
	}
	if report.Unresolved[0].Reason != "price_missing" {
		t.Fatalf("unresolved reason = %q, want price_missing", report.Unresolved[0].Reason)
	}

	unresolved := report.Unresolved[0]
	if err := app.AddFinanceEntry(ctx, 2001, bot.FinanceEntryInput{
		BookingID: unresolved.BookingID, Kind: "income", Category: "services", AmountCents: 1000, Currency: "EUR",
		OccurredAt: unresolved.StartAt, Description: "Усы", Source: "booking_override",
	}); err != nil {
		t.Fatalf("AddFinanceEntry override: %v", err)
	}
	report, err = app.FinanceReport(ctx, 2001, from, from.AddDate(0, 1, 0), "month")
	if err != nil {
		t.Fatalf("FinanceReport after override: %v", err)
	}
	if report.BookingIncomeCents != 5500 || len(report.Unresolved) != 0 {
		t.Fatalf("report after override = %#v, want 55 EUR income and no unresolved", report)
	}
}

func TestAppStore_DeleteServiceAndScheduleMonth(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Nails > Manicure > Classic", 30, ""); err != nil {
		t.Fatalf("AddService 1: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Nails > Manicure > Color", 45, ""); err != nil {
		t.Fatalf("AddService 2: %v", err)
	}
	if err := app.DeleteServiceByIndex(ctx, 2001, 2); err != nil {
		t.Fatalf("DeleteServiceByIndex: %v", err)
	}
	services, err := app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 1 || services[0].Name != "Classic" {
		t.Fatalf("services after delete = %#v, want only Classic", services)
	}
	if err := app.ReplaceServices(ctx, 2001, []bot.ServiceCatalogEntry{
		{Path: "Hair > Cut", DurationMin: 60, PriceText: "50 EUR"},
		{Path: "Hair > Color", DurationMin: 90, PriceText: "80 EUR"},
	}); err != nil {
		t.Fatalf("ReplaceServices: %v", err)
	}
	services, err = app.ListServices(ctx, 2001)
	if err != nil {
		t.Fatalf("ListServices after replace: %v", err)
	}
	if len(services) != 2 || services[0].Name != "Cut" || services[1].Name != "Color" || services[0].Description != "50 EUR" {
		t.Fatalf("services after replace = %#v", services)
	}

	juneStart := time.Date(2026, 6, 1, 10, 0, 0, 0, loc)
	julyStart := time.Date(2026, 7, 1, 10, 0, 0, 0, loc)
	for _, slotStart := range []time.Time{juneStart, juneStart.Add(30 * time.Minute), julyStart} {
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(30 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %s: %v", slotStart, err)
		}
	}
	result, err := app.DeleteScheduleMonth(ctx, 2001, time.Date(2026, 6, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatalf("DeleteScheduleMonth: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("deleted slots = %d, want 2", result.Deleted)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schedule_slots").Scan(&remaining); err != nil {
		t.Fatalf("count remaining slots: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining slots = %d, want 1", remaining)
	}
}

func TestAppStore_ScheduleChangesUseSuperAdminView(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleSuperAdmin, super.ID); err != nil {
		t.Fatalf("promote super: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetSuperAdminView(ctx, 1001, bot.SuperAdminView{Role: bot.RoleAdmin, AdminUsername: "master"}); err != nil {
		t.Fatalf("SetSuperAdminView: %v", err)
	}
	if err := app.SetSessionDuration(ctx, 1001, 15); err != nil {
		t.Fatalf("SetSessionDuration through view: %v", err)
	}
	if err := app.SetWeeklyHours(ctx, 1001, []bot.WeekdayHours{
		{Weekday: time.Thursday, Working: true, Start: "13:00", End: "14:00"},
	}); err != nil {
		t.Fatalf("SetWeeklyHours through view: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "last_free_slots", `["stale"]`); err != nil {
		t.Fatalf("seed last_free_slots: %v", err)
	}
	if err := repo.SetAdminSetting(ctx, admin.ID, "last_availability_slots", `[{"stale":true}]`); err != nil {
		t.Fatalf("seed last_availability_slots: %v", err)
	}

	result, err := app.GenerateSchedule(ctx, 1001, bot.GenerateScheduleRequest{
		Month:  time.Date(2026, 6, 1, 0, 0, 0, 0, loc),
		Months: 1,
	})
	if err != nil {
		t.Fatalf("GenerateSchedule through view: %v", err)
	}
	if result.Created == 0 {
		t.Fatal("created slots = 0, want target admin slots")
	}

	var adminSlots, superSlots int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schedule_slots WHERE admin_user_id = $1", admin.ID).Scan(&adminSlots); err != nil {
		t.Fatalf("count admin slots: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schedule_slots WHERE admin_user_id = $1", super.ID).Scan(&superSlots); err != nil {
		t.Fatalf("count super slots: %v", err)
	}
	if adminSlots != result.Created {
		t.Fatalf("admin slots = %d, want created %d", adminSlots, result.Created)
	}
	if superSlots != 0 {
		t.Fatalf("super slots = %d, want 0", superSlots)
	}
	var staleCache int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM admin_settings
WHERE admin_user_id = $1 AND key IN ('last_free_slots', 'last_availability_slots')
`, admin.ID).Scan(&staleCache); err != nil {
		t.Fatalf("count stale schedule cache: %v", err)
	}
	if staleCache != 0 {
		t.Fatalf("stale schedule cache rows = %d, want 0", staleCache)
	}

	deleteResult, err := app.DeleteScheduleMonth(ctx, 1001, time.Date(2026, 6, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatalf("DeleteScheduleMonth through view: %v", err)
	}
	if deleteResult.Deleted != result.Created {
		t.Fatalf("deleted slots = %d, want %d", deleteResult.Deleted, result.Created)
	}
}

func TestAppStore_AdminScheduleReminderAndMissingMonthNotice(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := app.RegisterOrUpdateUser(ctx, bot.UserRecord{TelegramID: 3001, Username: "client", Language: bot.LangRU}); err != nil {
		t.Fatalf("Register client: %v", err)
	}

	if err := app.PrepareUpcomingReminders(ctx, time.Date(2026, 6, 15, 11, 0, 0, 0, loc)); err != nil {
		t.Fatalf("PrepareUpcomingReminders: %v", err)
	}
	reminders, err := app.DueReminders(ctx, time.Date(2026, 6, 15, 11, 1, 0, 0, loc), 10)
	if err != nil {
		t.Fatalf("DueReminders after admin reminder: %v", err)
	}
	if len(reminders) != 1 || reminders[0].ChatID != 2001 {
		t.Fatalf("admin reminders = %#v, want one reminder for admin", reminders)
	}
	if err := app.MarkReminderSent(ctx, reminders[0].ID, time.Date(2026, 6, 15, 11, 2, 0, 0, loc)); err != nil {
		t.Fatalf("MarkReminderSent: %v", err)
	}

	if _, err := app.ListFreeSlotsForMonth(ctx, 3001, time.Date(2026, 8, 1, 0, 0, 0, 0, loc)); err != nil {
		t.Fatalf("ListFreeSlotsForMonth: %v", err)
	}
	reminders, err = app.DueReminders(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("DueReminders after missing month notice: %v", err)
	}
	if len(reminders) != 1 || reminders[0].ChatID != 2001 {
		t.Fatalf("reminders = %#v, want one missing month notice for admin", reminders)
	}
}

func TestAppStore_DailyAdminBookingSummary(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	client, err := repo.UpsertUser(ctx, 3001, "client", "Client")
	if err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	start := time.Date(2026, 6, 16, 10, 0, 0, 0, loc)
	slot, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
		AdminUserID: admin.ID,
		StartAt:     start,
		EndAt:       start.Add(time.Hour),
		Capacity:    1,
		Status:      domain.SlotStatusOpen,
	})
	if err != nil {
		t.Fatalf("CreateScheduleSlot: %v", err)
	}
	clientID := client.ID
	if _, err := repo.CreateBooking(ctx, domain.Booking{SlotID: slot.ID, UserID: &clientID, Status: domain.BookingStatusBooked}); err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	if err := app.PrepareUpcomingReminders(ctx, time.Date(2026, 6, 16, 7, 59, 0, 0, loc)); err != nil {
		t.Fatalf("PrepareUpcomingReminders before 8: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reminders WHERE kind = 'admin_daily_bookings'").Scan(&count); err != nil {
		t.Fatalf("count daily reminders before 8: %v", err)
	}
	if count != 0 {
		t.Fatalf("daily reminders before 8 = %d, want 0", count)
	}

	if err := app.PrepareUpcomingReminders(ctx, time.Date(2026, 6, 16, 8, 0, 0, 0, loc)); err != nil {
		t.Fatalf("PrepareUpcomingReminders at 8: %v", err)
	}
	var payload string
	if err := db.QueryRowContext(ctx, `
SELECT payload
FROM reminders
WHERE kind = 'admin_daily_bookings'
  AND chat_id = $1;
`, int64(2001)).Scan(&payload); err != nil {
		t.Fatalf("select daily reminder payload: %v", err)
	}
	if !strings.Contains(payload, "Записи на сегодня, 16.06.2026") ||
		!strings.Contains(payload, "10:00-11:00 - @client") {
		t.Fatalf("daily reminder payload = %q", payload)
	}

	if err := app.PrepareUpcomingReminders(ctx, time.Date(2026, 6, 16, 8, 1, 0, 0, loc)); err != nil {
		t.Fatalf("PrepareUpcomingReminders after 8: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reminders WHERE kind = 'admin_daily_bookings'").Scan(&count); err != nil {
		t.Fatalf("count daily reminders after 8: %v", err)
	}
	if count != 1 {
		t.Fatalf("daily reminders after repeated prepare = %d, want 1", count)
	}
}

func TestAppStore_RequestMissingMonth(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := app.RegisterOrUpdateUser(ctx, bot.UserRecord{TelegramID: 3001, Username: "client", Language: bot.LangRU}); err != nil {
		t.Fatalf("Register client: %v", err)
	}

	requested, err := app.RequestMissingMonth(ctx, 3001, time.Date(2026, 9, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatalf("RequestMissingMonth: %v", err)
	}
	if !requested {
		t.Fatal("requested = false, want true for month without schedule")
	}
	reminders, err := app.DueReminders(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("DueReminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].ChatID != 2001 {
		t.Fatalf("reminders = %#v, want one admin notice", reminders)
	}

	if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
		AdminUserID: admin.ID,
		StartAt:     time.Date(2026, 10, 5, 10, 0, 0, 0, loc),
		EndAt:       time.Date(2026, 10, 5, 10, 30, 0, 0, loc),
		Capacity:    1,
		Status:      domain.SlotStatusOpen,
	}); err != nil {
		t.Fatalf("CreateScheduleSlot: %v", err)
	}
	requested, err = app.RequestMissingMonth(ctx, 3001, time.Date(2026, 10, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatalf("RequestMissingMonth existing: %v", err)
	}
	if requested {
		t.Fatal("requested = true, want false for existing schedule month")
	}
}

func TestBotE2E_ClientInteractiveBookingWithCategories(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	loc := time.UTC
	app := newAppStore(db, repo, loc)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetProfileText(ctx, 2001, "Мастер тестовой записи"); err != nil {
		t.Fatalf("SetProfileText: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Ногти > Маникюр > Классический", 30, ""); err != nil {
		t.Fatalf("AddService 1: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Ногти > Маникюр > Покрытие", 45, ""); err != nil {
		t.Fatalf("AddService 2: %v", err)
	}

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 8; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}

	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	client := telegram.User{ID: 3001, Username: "client", FirstName: "Client"}
	chat := telegram.Chat{ID: 3001}

	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: client,
		Chat: chat,
		Text: "/start",
	}); err != nil {
		t.Fatalf("HandleMessage(/start): %v", err)
	}
	callbacks := []string{
		"lang:ru",
		"bookstart",
		"cat:1",
		"sub:1",
		"svc:1",
		"more:no",
		"time:nearest",
	}
	for _, data := range callbacks {
		if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
			ID:   "callback-" + data,
			From: client,
			Message: &telegram.Message{
				Chat: chat,
			},
			Data: data,
		}); err != nil {
			t.Fatalf("HandleCallback(%q): %v", data, err)
		}
	}
	if len(tg.photos) != 1 || !strings.Contains(tg.photos[0].Caption, "Выберите дату") {
		t.Fatalf("client availability overview = %#v", tg.photos)
	}
	dateCallback := ""
	if tg.photos[0].ReplyMarkup != nil {
		for _, row := range tg.photos[0].ReplyMarkup.InlineKeyboard {
			for _, button := range row {
				if strings.HasPrefix(button.CallbackData, "slotdate:") {
					dateCallback = button.CallbackData
					break
				}
			}
		}
	}
	if dateCallback == "" {
		t.Fatalf("availability date keyboard = %#v", tg.photos[0].ReplyMarkup)
	}
	for _, data := range []string{dateCallback, "slot:1"} {
		if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
			ID: data, From: client, Message: &telegram.Message{Chat: chat}, Data: data,
		}); err != nil {
			t.Fatalf("HandleCallback(%q): %v", data, err)
		}
	}
	var bookedBeforeConfirmation int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status = 'booked'").Scan(&bookedBeforeConfirmation); err != nil {
		t.Fatalf("count booked before confirmation: %v", err)
	}
	if bookedBeforeConfirmation != 0 {
		t.Fatalf("booked before confirmation = %d, want 0", bookedBeforeConfirmation)
	}
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID:   "callback-bookconfirm:yes",
		From: client,
		Message: &telegram.Message{
			Chat: chat,
		},
		Data: "bookconfirm:yes",
	}); err != nil {
		t.Fatalf("HandleCallback(bookconfirm:yes): %v", err)
	}

	messages := tg.texts()
	if len(messages) == 0 {
		t.Fatal("bot sent no messages")
	}
	if !strings.Contains(messages[len(messages)-1], "Вы записаны") {
		t.Fatalf("last bot message = %q, want booking confirmation; all messages: %#v", messages[len(messages)-1], messages)
	}

	var booked, blocked int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status = 'booked'").Scan(&booked); err != nil {
		t.Fatalf("count booked: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status = 'blocked'").Scan(&blocked); err != nil {
		t.Fatalf("count blocked: %v", err)
	}
	if booked != 1 || blocked != 1 {
		t.Fatalf("booked=%d blocked=%d, want one 30-minute booking over two 15-minute slots", booked, blocked)
	}

	bookings, err := app.ListMyBookings(ctx, 3001, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListMyBookings: %v", err)
	}
	if len(bookings) != 1 || !bookings[0].StartAt.Equal(start) || !bookings[0].EndAt.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("bookings = %#v, want one booking from %s to %s", bookings, start, start.Add(30*time.Minute))
	}
}

func TestBotE2E_AdminNaturalBookingFromTextVoiceAndImageRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, 3001, "client", "Client"); err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Основное > Эпиляция", 30, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 4; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}

	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	bookingBot.SetAdminBookingIntentParser(staticAdminBookingParser{intent: nlu.AdminBookingIntent{
		IsCreateBooking: true,
		ContactType:     "telegram",
		Contact:         "@client",
		ServiceIndexes:  []int{1},
		DurationMin:     30,
		StartAt:         start.Format(time.RFC3339),
		Confidence:      0.98,
	}})
	adminUser := telegram.User{ID: 2001, Username: "master", FirstName: "Master"}
	chat := telegram.Chat{ID: 2001}

	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: adminUser,
		Chat: chat,
		Text: "запиши @client на эпиляцию",
	}); err != nil {
		t.Fatalf("HandleMessage text: %v", err)
	}
	assertAdminBookingProposalCount(t, tg, 1)
	assertBookedCount(t, ctx, db, 0)
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID:   "admin-text-no",
		From: adminUser,
		Message: &telegram.Message{
			Chat: chat,
		},
		Data: "bookconfirm:no",
	}); err != nil {
		t.Fatalf("HandleCallback text no: %v", err)
	}

	bookingBot.SetSpeechRecognizer(staticSpeechRecognizer{text: "запиши @client на эпиляцию"})
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From:  adminUser,
		Chat:  chat,
		Voice: &telegram.Voice{FileID: "voice", FileSize: 10, Duration: 2},
	}); err != nil {
		t.Fatalf("HandleMessage voice: %v", err)
	}
	assertAdminBookingProposalCount(t, tg, 2)
	assertBookedCount(t, ctx, db, 0)
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID:   "admin-voice-no",
		From: adminUser,
		Message: &telegram.Message{
			Chat: chat,
		},
		Data: "bookconfirm:no",
	}); err != nil {
		t.Fatalf("HandleCallback voice no: %v", err)
	}

	bookingBot.SetImageTextRecognizer(staticImageTextRecognizer{text: "запиши @client на эпиляцию"})
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From:  adminUser,
		Chat:  chat,
		Photo: []telegram.PhotoSize{{FileID: "photo", Width: 640, Height: 480, FileSize: 10}},
	}); err != nil {
		t.Fatalf("HandleMessage image: %v", err)
	}
	assertAdminBookingProposalCount(t, tg, 3)
	assertBookedCount(t, ctx, db, 0)
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID:   "admin-confirm",
		From: adminUser,
		Message: &telegram.Message{
			Chat: chat,
		},
		Data: "bookconfirm:yes",
	}); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	assertBookedCount(t, ctx, db, 1)
}

func TestBotE2E_AdminNaturalMonthlyScheduleFromVoiceRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.SetSessionDuration(ctx, 2001, 30); err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(time.Local)
	target := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	bookingBot.SetAdminBookingIntentParser(staticAdminBookingParser{schedulePlanIntent: nlu.AdminSchedulePlanIntent{
		IsSchedulePlan: true, TargetMonth: target.Format("2006-01"), Confidence: 0.98,
		Rules: []nlu.AdminSchedulePlanRule{{Weekdays: []int{1, 2, 3, 4, 5}, Start: "10:00", End: "17:00"}},
	}})
	bookingBot.SetSpeechRecognizer(staticSpeechRecognizer{text: "сделай расписание на следующий месяц по будням с 10 до 17"})
	adminUser := telegram.User{ID: 2001, Username: "master", FirstName: "Master"}
	chat := telegram.Chat{ID: 2001}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: adminUser, Chat: chat, Voice: &telegram.Voice{FileID: "schedule-plan", FileSize: 10, Duration: 3},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := app.GetConversationState(ctx, 2001)
	if err != nil || state.Step != "admin_schedule_plan_confirm" || len(state.SchedulePlan.Days) < 20 || len(tg.photos) != 1 {
		t.Fatalf("schedule plan state=%#v photos=%d err=%v", state, len(tg.photos), err)
	}
	var before int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schedule_slots WHERE admin_user_id=$1", admin.ID).Scan(&before); err != nil || before != 0 {
		t.Fatalf("slots before confirmation=%d err=%v", before, err)
	}
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID: "schedule-plan-confirm", From: adminUser, Message: &telegram.Message{Chat: chat}, Data: "scheduleplan:yes",
	}); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schedule_slots WHERE admin_user_id=$1", admin.ID).Scan(&after); err != nil || after == 0 {
		t.Fatalf("slots after confirmation=%d err=%v", after, err)
	}
}

func TestBotE2E_ScheduleImageImportReviewsEntriesOneByOne(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Основное > Электроэпиляция", 30, "25 €"); err != nil {
		t.Fatalf("AddService: %v", err)
	}

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	correctedStart := start.Add(2 * time.Hour)
	correctedLocalStart := time.Date(correctedStart.Year(), correctedStart.Month(), correctedStart.Day(), correctedStart.Hour(), correctedStart.Minute(), 0, 0, time.Local)
	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	bookingBot.SetAdminBookingIntentParser(staticAdminBookingParser{
		scheduleIntent: nlu.AdminScheduleImportIntent{
			IsSchedule: true,
			Confidence: 0.98,
			Entries: []nlu.AdminScheduleImportEntry{
				{Client: "Лиза", ServiceIndexes: []int{1}, ServiceQueries: []string{"электро"}, DurationMin: 30, StartAt: start.Format(time.RFC3339), Confidence: 0.98},
				{Client: "Катя", ServiceQueries: []string{"неизвестная услуга"}, DurationMin: 30, StartAt: start.Add(time.Hour).Format(time.RFC3339), Confidence: 0.98},
			},
		},
		editIntent: nlu.AdminScheduleEditIntent{
			IsEdit: true, ChangeService: true, Services: []nlu.AdminScheduleEditService{{ServiceIndexes: []int{1}, ServiceQueries: []string{"электро"}, DurationMin: 30}},
			ChangeStartAt: true, StartAt: correctedLocalStart.Format(time.RFC3339), Confidence: 0.98,
		},
	})
	bookingBot.SetImageTextRecognizer(staticImageTextRecognizer{text: "Неделя 34\nпн 9:30 Лиза электро\n10:30 Катя услуга"})
	adminUser := telegram.User{ID: 2001, Username: "master", FirstName: "Master"}
	chat := telegram.Chat{ID: 2001}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: adminUser, Chat: chat,
		Photo: []telegram.PhotoSize{{FileID: "schedule", Width: 640, Height: 480, FileSize: 10}},
	}); err != nil {
		t.Fatalf("HandleMessage image: %v", err)
	}
	messages := tg.texts()
	if !strings.Contains(messages[len(messages)-1], "Проверка записи 1 из 2") || strings.Contains(messages[len(messages)-1], "Катя") {
		t.Fatalf("first review message = %q", messages[len(messages)-1])
	}
	assertBookedCount(t, ctx, db, 0)

	callback := func(data string) {
		t.Helper()
		if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
			ID: data, From: adminUser, Message: &telegram.Message{Chat: chat}, Data: data,
		}); err != nil {
			t.Fatalf("HandleCallback(%q): %v", data, err)
		}
	}
	callback("scheduleimport:save:0")
	assertBookedCount(t, ctx, db, 1)
	messages = tg.texts()
	if !strings.Contains(messages[len(messages)-1], "Проверка записи 2 из 2") || !strings.Contains(messages[len(messages)-1], "Катя") {
		t.Fatalf("second review message = %q", messages[len(messages)-1])
	}

	callback("scheduleimport:save:0")
	assertBookedCount(t, ctx, db, 1)
	state, err := app.GetConversationState(ctx, 2001)
	if err != nil || state.ScheduleImportIndex != 1 {
		t.Fatalf("state after stale callback = %#v, %v", state, err)
	}

	callback("scheduleimport:edit:1")
	messages = tg.texts()
	if !strings.Contains(messages[len(messages)-1], "обычной фразой") || strings.Contains(messages[len(messages)-1], "|") {
		t.Fatalf("editable record prompt = %q", messages[len(messages)-1])
	}
	corrected := "перенеси на два часа позже и поставь электро"
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: adminUser, Chat: chat, Text: corrected}); err != nil {
		t.Fatalf("edit whole record: %v", err)
	}
	messages = tg.texts()
	if !strings.Contains(messages[len(messages)-1], "Запись готова к сохранению") {
		t.Fatalf("edited review message = %q", messages[len(messages)-1])
	}
	state, err = app.GetConversationState(ctx, 2001)
	correctedStateStart, parseErr := time.Parse(time.RFC3339, state.ScheduleImportEntries[1].StartAt)
	if err != nil || parseErr != nil || correctedStateStart.Format("02.01.2006 15:04") != correctedStart.Format("02.01.2006 15:04") || len(state.ScheduleImportEntries[1].ServiceIndexes) != 1 {
		t.Fatalf("corrected import state = %#v, %v", state, err)
	}

	bookingBot.SetAdminBookingIntentParser(staticAdminBookingParser{editIntent: nlu.AdminScheduleEditIntent{
		IsEdit: true, ChangeClient: true, Client: "Екатерина", Confidence: 0.98,
	}})
	bookingBot.SetSpeechRecognizer(staticSpeechRecognizer{text: "это Екатерина"})
	callback("scheduleimport:edit:1")
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: adminUser, Chat: chat,
		Voice: &telegram.Voice{FileID: "voice-correction", FileSize: 10, Duration: 2},
	}); err != nil {
		t.Fatalf("edit record by voice: %v", err)
	}
	state, err = app.GetConversationState(ctx, 2001)
	if err != nil || state.ScheduleImportEntries[1].Client != "Екатерина" || state.ScheduleImportEntries[1].StartAt != correctedStateStart.Format(time.RFC3339) || len(state.ScheduleImportEntries[1].ServiceIndexes) != 1 {
		t.Fatalf("voice-corrected import state = %#v, %v", state, err)
	}
	callback("scheduleimport:skip:1")
	assertBookedCount(t, ctx, db, 1)
	messages = tg.texts()
	if !strings.Contains(messages[len(messages)-1], "добавлено 1, пропущено 1") {
		t.Fatalf("completion message = %q", messages[len(messages)-1])
	}
}

func TestBotE2E_NaturalBookingDraftKeepsRecognizedFields(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, 3001, "client", "Client"); err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Основное > Эпиляция", 30, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	if _, err := app.UpsertContactAlias(ctx, 2001, "Лиза", "telegram", "@client"); err != nil {
		t.Fatalf("UpsertContactAlias: %v", err)
	}
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	for i := 0; i < 4; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}

	adminTG := &fakeTelegramClient{}
	adminBot := bot.New(adminTG, app, log.New(io.Discard, "", 0), "tim1106")
	adminBot.SetAdminBookingIntentParser(staticAdminBookingParser{
		intent: nlu.AdminBookingIntent{
			IsCreateBooking: true,
			ContactType:     "unknown",
			Contact:         "Лиза",
			StartAt:         start.Format(time.RFC3339),
			Confidence:      0.95,
		},
		editIntent: nlu.AdminScheduleEditIntent{
			IsEdit: true, ChangeStartAt: true, StartAt: start.Add(15 * time.Minute).Format(time.RFC3339), Confidence: 0.95,
		},
	})
	adminUser := telegram.User{ID: 2001, Username: "master", FirstName: "Master"}
	adminChat := telegram.Chat{ID: 2001}
	if err := adminBot.HandleMessage(ctx, &telegram.Message{From: adminUser, Chat: adminChat, Text: "запиши Лизу завтра"}); err != nil {
		t.Fatalf("admin partial request: %v", err)
	}
	adminState, err := app.GetConversationState(ctx, 2001)
	if err != nil || adminState.BookingDraft != "admin" || adminState.Username != "client" || adminState.FromDateTime == "" || len(adminState.ServiceIndexes) != 0 {
		t.Fatalf("admin draft = %#v, err=%v", adminState, err)
	}
	if err := adminBot.HandleMessage(ctx, &telegram.Message{From: adminUser, Chat: adminChat, Text: "эпиляция"}); err != nil {
		t.Fatalf("admin service correction: %v", err)
	}
	if err := adminBot.HandleMessage(ctx, &telegram.Message{From: adminUser, Chat: adminChat, Text: "Нет"}); err != nil {
		t.Fatalf("admin finish service correction: %v", err)
	}
	assertAdminBookingProposalCount(t, adminTG, 1)
	if err := adminBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID: "admin-edit", From: adminUser, Message: &telegram.Message{Chat: adminChat}, Data: "bookconfirm:edit",
	}); err != nil {
		t.Fatalf("admin edit booking: %v", err)
	}
	adminState, err = app.GetConversationState(ctx, 2001)
	if err != nil || adminState.Step != "booking_edit" || len(adminState.ServiceIndexes) != 1 {
		t.Fatalf("admin edit state = %#v, err=%v", adminState, err)
	}
	adminBot.SetSpeechRecognizer(staticSpeechRecognizer{text: "на пятнадцать минут позже"})
	if err := adminBot.HandleMessage(ctx, &telegram.Message{
		From: adminUser, Chat: adminChat,
		Voice: &telegram.Voice{FileID: "admin-booking-correction", FileSize: 10, Duration: 2},
	}); err != nil {
		t.Fatalf("admin voice booking correction: %v", err)
	}
	adminState, err = app.GetConversationState(ctx, 2001)
	adminCorrectedStart, parseErr := time.Parse(time.RFC3339, adminState.FromDateTime)
	if err != nil || parseErr != nil || adminState.Step != "booking_confirm" || !adminCorrectedStart.Equal(start.Add(15*time.Minute)) {
		t.Fatalf("admin corrected state = %#v, err=%v", adminState, err)
	}
	assertAdminBookingProposalCount(t, adminTG, 2)

	clientTG := &fakeTelegramClient{}
	clientBot := bot.New(clientTG, app, log.New(io.Discard, "", 0), "tim1106")
	clientBot.SetBookingIntentParser(staticBookingParser{intent: nlu.BookingIntent{
		IsBooking:  true,
		DateFrom:   start.Format("2006-01-02"),
		DateTo:     start.AddDate(0, 0, 1).Format("2006-01-02"),
		Period:     "all",
		Confidence: 0.95,
	}})
	clientBot.SetAdminBookingIntentParser(staticAdminBookingParser{editIntent: nlu.AdminScheduleEditIntent{
		IsEdit: true, ChangeStartAt: true, StartAt: start.Add(15 * time.Minute).Format(time.RFC3339), Confidence: 0.95,
	}})
	clientUser := telegram.User{ID: 3001, Username: "client", FirstName: "Client"}
	clientChat := telegram.Chat{ID: 3001}
	if err := clientBot.HandleMessage(ctx, &telegram.Message{From: clientUser, Chat: clientChat, Text: "хочу записаться завтра"}); err != nil {
		t.Fatalf("client partial request: %v", err)
	}
	clientState, err := app.GetConversationState(ctx, 3001)
	if err != nil || clientState.BookingDraft != "client" || clientState.DateFrom != start.Format("2006-01-02") || len(clientState.ServiceIndexes) != 0 {
		t.Fatalf("client draft = %#v, err=%v", clientState, err)
	}
	if err := clientBot.HandleMessage(ctx, &telegram.Message{From: clientUser, Chat: clientChat, Text: "эпиляция"}); err != nil {
		t.Fatalf("client service correction: %v", err)
	}
	if err := clientBot.HandleMessage(ctx, &telegram.Message{From: clientUser, Chat: clientChat, Text: "Нет"}); err != nil {
		t.Fatalf("client finish service correction: %v", err)
	}
	clientState, err = app.GetConversationState(ctx, 3001)
	if err != nil || clientState.Step != "slot" || clientState.DateFrom != start.Format("2006-01-02") || len(clientState.ServiceIndexes) != 1 {
		t.Fatalf("client slot state = %#v, err=%v", clientState, err)
	}
	if err := clientBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID: "client-slot", From: clientUser, Message: &telegram.Message{Chat: clientChat}, Data: "slot:1",
	}); err != nil {
		t.Fatalf("client choose slot: %v", err)
	}
	if err := clientBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID: "client-edit", From: clientUser, Message: &telegram.Message{Chat: clientChat}, Data: "bookconfirm:edit",
	}); err != nil {
		t.Fatalf("client edit booking: %v", err)
	}
	clientBot.SetSpeechRecognizer(staticSpeechRecognizer{text: "на пятнадцать минут позже"})
	if err := clientBot.HandleMessage(ctx, &telegram.Message{
		From: clientUser, Chat: clientChat,
		Voice: &telegram.Voice{FileID: "client-booking-correction", FileSize: 10, Duration: 2},
	}); err != nil {
		t.Fatalf("client voice booking correction: %v", err)
	}
	clientState, err = app.GetConversationState(ctx, 3001)
	clientCorrectedStart, parseErr := time.Parse(time.RFC3339, clientState.FromDateTime)
	if err != nil || parseErr != nil || clientState.Step != "booking_confirm" || !clientCorrectedStart.Equal(start.Add(15*time.Minute)) {
		t.Fatalf("client corrected state = %#v, err=%v", clientState, err)
	}
}

func TestAppStore_ContactAliasRelinksExistingNamedBooking(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Зоны > Подмышки", 30, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour).Add(10 * time.Minute)
	for i := 0; i < 3; i++ {
		slotStart := start.Add(-10 * time.Minute).Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID, StartAt: slotStart, EndAt: slotStart.Add(15 * time.Minute),
			Capacity: 1, Status: domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}
	services, err := app.ListServices(ctx, 2001)
	if err != nil || len(services) != 1 {
		t.Fatalf("ListServices = %#v, %v", services, err)
	}
	conflict, err := app.FindImportBookingConflict(ctx, 2001, []int{1}, start)
	if err != nil || conflict != nil {
		t.Fatalf("FindImportBookingConflict before import = %#v, %v", conflict, err)
	}
	if _, err := app.AddImportedBooking(ctx, 2001, "name", "Анастасия Балтаджи", []int{1}, start); err != nil {
		t.Fatalf("AddImportedBooking name: %v", err)
	}
	var closedOverlaps int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule_slots WHERE admin_user_id = $1 AND note = '' AND status = 'closed'`, admin.ID).Scan(&closedOverlaps); err != nil || closedOverlaps != 3 {
		t.Fatalf("closed overlapping slots = %d, %v", closedOverlaps, err)
	}
	conflict, err = app.FindImportBookingConflict(ctx, 2001, []int{1}, start.Add(15*time.Minute))
	if err != nil || conflict == nil {
		t.Fatalf("FindImportBookingConflict overlap = %#v, %v", conflict, err)
	}
	if conflict.Username != "Анастасия Балтаджи" || conflict.StartAt != start || conflict.EndAt != start.Add(30*time.Minute) || conflict.Blocked {
		t.Fatalf("conflict details = %#v", conflict)
	}
	if len(conflict.ServiceNames) != 1 || conflict.ServiceNames[0] != services[0].Name {
		t.Fatalf("conflict services = %v", conflict.ServiceNames)
	}
	before, err := app.ListAdminBookingsRange(ctx, 2001, start.Add(-time.Minute), start.Add(time.Hour))
	if err != nil || len(before) != 1 || before[0].Username != "Анастасия Балтаджи" {
		t.Fatalf("bookings before alias = %#v, %v", before, err)
	}
	updated, err := app.UpsertContactAlias(ctx, 2001, "Анастасия Балтаджи", "telegram", "@hasti69")
	if err != nil || updated != 1 {
		t.Fatalf("UpsertContactAlias = %d, %v", updated, err)
	}
	after, err := app.ListAdminBookingsRange(ctx, 2001, start.Add(-time.Minute), start.Add(time.Hour))
	if err != nil || len(after) != 1 || after[0].Username != "hasti69" {
		t.Fatalf("bookings after alias = %#v, %v", after, err)
	}
}

func TestBotE2E_AdminNaturalScheduleSendsWeekImageWithBooking(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Основное > Эпиляция", 90, ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	start := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		slotStart := start.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID,
			StartAt:     slotStart,
			EndAt:       slotStart.Add(15 * time.Minute),
			Capacity:    1,
			Status:      domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot %d: %v", i, err)
		}
	}
	if _, err := app.ListServices(ctx, 2001); err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	available, err := app.ListFreeSlotsForServicesRange(ctx, 2001, []int{1}, start.Add(-time.Hour), start.Add(3*time.Hour))
	if err != nil || len(available) != 1 {
		t.Fatalf("ListFreeSlotsForServicesRange = %#v, %v", available, err)
	}
	if _, err := app.AddBookingForContactByIndex(ctx, 2001, "phone", "+35799999999", 1); err != nil {
		t.Fatalf("AddBookingForContactByIndex: %v", err)
	}

	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: telegram.User{ID: 2001, Username: "master", FirstName: "Master"},
		Chat: telegram.Chat{ID: 2001},
		Text: "покажи график за 25.08.2026",
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(tg.photos) != 1 {
		t.Fatalf("photos = %d, want 1", len(tg.photos))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(tg.photos[0].Photo))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 1600 || cfg.Height < 1000 {
		t.Fatalf("week image = %dx%d, want compact full-week PNG", cfg.Width, cfg.Height)
	}
	if !strings.Contains(tg.photos[0].Caption, "24.08") || !strings.Contains(tg.photos[0].Caption, "30.08") {
		t.Fatalf("caption = %q, want selected week", tg.photos[0].Caption)
	}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: telegram.User{ID: 2001, Username: "master", FirstName: "Master"},
		Chat: telegram.Chat{ID: 2001},
		Text: "Свободное время",
	}); err != nil {
		t.Fatalf("free time menu: %v", err)
	}
	if len(tg.photos) != 2 {
		t.Fatalf("photos after free time menu = %d, want 2", len(tg.photos))
	}
	freeCfg, err := png.DecodeConfig(bytes.NewReader(tg.photos[1].Photo))
	if err != nil {
		t.Fatalf("DecodeConfig free time: %v", err)
	}
	if freeCfg.Width != 1600 || freeCfg.Height < 1000 {
		t.Fatalf("free time image = %dx%d, want seven-day PNG", freeCfg.Width, freeCfg.Height)
	}
	selfBookingStart := start.AddDate(0, 0, 1)
	for i := 0; i < 20; i++ {
		slotStart := selfBookingStart.Add(time.Duration(i*15) * time.Minute)
		if _, err := repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
			AdminUserID: admin.ID, StartAt: slotStart, EndAt: slotStart.Add(15 * time.Minute),
			Capacity: 1, Status: domain.SlotStatusOpen,
		}); err != nil {
			t.Fatalf("CreateScheduleSlot for admin self-booking %d: %v", i, err)
		}
	}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: telegram.User{ID: 2001, Username: "master", FirstName: "Master"},
		Chat: telegram.Chat{ID: 2001}, Text: "Записаться",
	}); err != nil {
		t.Fatalf("admin self-booking start: %v", err)
	}
	for _, data := range []string{"cat:1", "sub:1", "svc:1", "more:no", "time:nearest"} {
		if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
			ID:      data,
			From:    telegram.User{ID: 2001, Username: "master", FirstName: "Master"},
			Message: &telegram.Message{Chat: telegram.Chat{ID: 2001}}, Data: data,
		}); err != nil {
			t.Fatalf("admin self-booking callback %q: %v", data, err)
		}
	}
	if len(tg.photos) != 3 || !strings.Contains(tg.photos[2].Caption, "Выберите дату") {
		t.Fatalf("admin self-booking availability photos = %#v", tg.photos)
	}
	dateCallback := ""
	for _, row := range tg.photos[2].ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "slotdate:") {
				dateCallback = button.CallbackData
				break
			}
		}
	}
	if dateCallback == "" {
		t.Fatalf("admin self-booking date keyboard = %#v", tg.photos[2].ReplyMarkup)
	}
	if err := bookingBot.HandleCallback(ctx, &telegram.CallbackQuery{
		ID:      dateCallback,
		From:    telegram.User{ID: 2001, Username: "master", FirstName: "Master"},
		Message: &telegram.Message{Chat: telegram.Chat{ID: 2001}}, Data: dateCallback,
	}); err != nil {
		t.Fatalf("admin self-booking date callback: %v", err)
	}
	if len(tg.photos) != 4 || !strings.Contains(tg.photos[3].Caption, "Выберите время под картинкой") {
		t.Fatalf("admin self-booking time image = %#v", tg.photos)
	}
	timePhoto := tg.photos[3]
	timeButtons := 0
	foundNextPage := false
	for _, row := range timePhoto.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "slot:") {
				timeButtons++
			}
			if button.CallbackData == "slotpage:next" {
				foundNextPage = true
			}
		}
	}
	if timeButtons > 9 || !foundNextPage {
		t.Fatalf("admin self-booking time keyboard has %d time buttons, next=%v: %#v", timeButtons, foundNextPage, timePhoto.ReplyMarkup)
	}
}

func TestBotE2E_StartOnboardingShowsServicesWithoutCommands(t *testing.T) {
	ctx := context.Background()
	db := openAppStorePostgresContainer(t, ctx)
	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	app := newAppStore(db, repo, time.UTC)
	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := app.SetProfileText(ctx, 2001, "Работаю бережно в центре города"); err != nil {
		t.Fatalf("SetProfileText: %v", err)
	}
	if err := app.AddService(ctx, 2001, "Эпиляция > Ноги > Голени", 45, "35 EUR"); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, 3001, "client", "Client"); err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}
	if err := app.SetUserLanguage(ctx, 3001, bot.LangRU); err != nil {
		t.Fatalf("SetUserLanguage: %v", err)
	}

	tg := &fakeTelegramClient{}
	bookingBot := bot.New(tg, app, log.New(io.Discard, "", 0), "tim1106")
	greetingGenerator := &staticClientGreetingGenerator{text: "Анна, добро пожаловать! Подберём подходящую процедуру и удобное время."}
	bookingBot.SetClientGreetingGenerator(greetingGenerator)
	client := telegram.User{ID: 3001, Username: "client", FirstName: "Анна"}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: client, Chat: telegram.Chat{ID: 3001}, Text: "/start"}); err != nil {
		t.Fatalf("client /start: %v", err)
	}
	if len(tg.messages) != 1 {
		t.Fatalf("client start messages = %d, want 1", len(tg.messages))
	}
	startMessage := tg.messages[0]
	for _, want := range []string{"Анна, добро пожаловать!", "Доступные услуги:", "Эпиляция / Ноги / Голени", "35 EUR"} {
		if !strings.Contains(startMessage.Text, want) {
			t.Fatalf("client start = %q, missing %q", startMessage.Text, want)
		}
	}
	if !strings.Contains(greetingGenerator.request.MasterDescription, "Работаю бережно") || len(greetingGenerator.request.Services) != 1 || greetingGenerator.request.Services[0].Name != "Голени" {
		t.Fatalf("greeting request = %#v", greetingGenerator.request)
	}
	if strings.Contains(startMessage.Text, "/help") || strings.Contains(startMessage.Text, "/book") {
		t.Fatalf("client start exposes commands: %q", startMessage.Text)
	}
	if startMessage.ReplyMarkup == nil || len(startMessage.ReplyMarkup.Keyboard) != 3 || startMessage.ReplyMarkup.Keyboard[0][0].Text != "Начать запись" || startMessage.ReplyMarkup.Keyboard[1][0].Text != "Календарь" || startMessage.ReplyMarkup.Keyboard[2][0].Text != "Мои записи" {
		t.Fatalf("client start keyboard = %#v", startMessage.ReplyMarkup)
	}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: client, Chat: telegram.Chat{ID: 3001}, Text: "Календарь"}); err != nil {
		t.Fatalf("client calendar button: %v", err)
	}
	if len(tg.photos) != 1 {
		t.Fatalf("client calendar photos = %d, want 1", len(tg.photos))
	}
	if _, err := png.DecodeConfig(bytes.NewReader(tg.photos[0].Photo)); err != nil {
		t.Fatalf("decode client calendar: %v", err)
	}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: client, Chat: telegram.Chat{ID: 3001}, Text: "/week 2026-08-24"}); err != nil {
		t.Fatalf("client week: %v", err)
	}
	if len(tg.photos) != 2 || !strings.Contains(tg.photos[1].Caption, "данные записей скрыты") {
		t.Fatalf("client week output = %#v", tg.photos)
	}
	bookingBot.SetClientGreetingGenerator(&staticClientGreetingGenerator{err: fmt.Errorf("generator unavailable")})
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: client, Chat: telegram.Chat{ID: 3001}, Text: "/start"}); err != nil {
		t.Fatalf("client /start with greeting fallback: %v", err)
	}
	fallbackMessage := tg.messages[len(tg.messages)-1].Text
	fallbackGreeting := strings.SplitN(fallbackMessage, "Доступные услуги:", 2)[0]
	if !strings.Contains(fallbackGreeting, "Здесь можно выбрать услугу") || strings.Contains(strings.ToLower(fallbackGreeting), "эпиляц") {
		t.Fatalf("client fallback greeting = %q", fallbackMessage)
	}
	if err := bookingBot.HandleMessage(ctx, &telegram.Message{From: client, Chat: telegram.Chat{ID: 3001}, Text: "Начать запись"}); err != nil {
		t.Fatalf("start booking button: %v", err)
	}
	if !strings.Contains(tg.messages[len(tg.messages)-1].Text, "Выберите категорию") {
		t.Fatalf("guided booking did not start: %#v", tg.texts())
	}

	if err := bookingBot.HandleMessage(ctx, &telegram.Message{
		From: telegram.User{ID: 2001, Username: "master", FirstName: "Мастер"},
		Chat: telegram.Chat{ID: 2001},
		Text: "/start",
	}); err != nil {
		t.Fatalf("admin /start: %v", err)
	}
	adminMessage := tg.messages[len(tg.messages)-1]
	for _, want := range []string{"Основные действия доступны без команд", "запиши @client на эпиляцию", "покажи график"} {
		if !strings.Contains(adminMessage.Text, want) {
			t.Fatalf("admin start = %q, missing %q", adminMessage.Text, want)
		}
	}
	for _, row := range adminMessage.ReplyMarkup.Keyboard {
		for _, button := range row {
			if strings.HasPrefix(button.Text, "/") {
				t.Fatalf("admin start exposes command button %q", button.Text)
			}
		}
	}
}

type staticAdminBookingParser struct {
	intent             nlu.AdminBookingIntent
	scheduleIntent     nlu.AdminScheduleImportIntent
	editIntent         nlu.AdminScheduleEditIntent
	schedulePlanIntent nlu.AdminSchedulePlanIntent
}

type staticBookingParser struct {
	intent nlu.BookingIntent
}

type staticClientGreetingGenerator struct {
	text    string
	err     error
	request nlu.ClientGreetingRequest
}

func (g *staticClientGreetingGenerator) GenerateClientGreeting(_ context.Context, req nlu.ClientGreetingRequest) (string, error) {
	g.request = req
	return g.text, g.err
}

func (p staticBookingParser) ParseBookingIntent(context.Context, nlu.BookingIntentRequest) (nlu.BookingIntent, error) {
	return p.intent, nil
}

func (p staticAdminBookingParser) ParseAdminBookingIntent(context.Context, nlu.AdminBookingIntentRequest) (nlu.AdminBookingIntent, error) {
	return p.intent, nil
}

func (p staticAdminBookingParser) ParseAdminScheduleImport(context.Context, nlu.AdminScheduleImportRequest) (nlu.AdminScheduleImportIntent, error) {
	return p.scheduleIntent, nil
}

func (p staticAdminBookingParser) ParseAdminScheduleEdit(context.Context, nlu.AdminScheduleEditRequest) (nlu.AdminScheduleEditIntent, error) {
	return p.editIntent, nil
}

func (p staticAdminBookingParser) ParseAdminSchedulePlan(context.Context, nlu.AdminSchedulePlanRequest) (nlu.AdminSchedulePlanIntent, error) {
	return p.schedulePlanIntent, nil
}

type staticSpeechRecognizer struct {
	text string
}

func (r staticSpeechRecognizer) Transcribe(context.Context, nlu.SpeechRequest) (string, error) {
	return r.text, nil
}

type staticImageTextRecognizer struct {
	text string
}

func (r staticImageTextRecognizer) RecognizeText(context.Context, nlu.ImageTextRequest) (string, error) {
	return r.text, nil
}

func assertAdminBookingProposalCount(t *testing.T, tg *fakeTelegramClient, want int) {
	t.Helper()
	count := 0
	for _, message := range tg.texts() {
		if strings.Contains(message, "Подтвердите запись") && strings.Contains(message, "@client") {
			count++
		}
	}
	if count != want {
		t.Fatalf("admin booking proposals = %d, want %d; messages=%#v", count, want, tg.texts())
	}
}

func assertBookedCount(t *testing.T, ctx context.Context, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookings WHERE status = 'booked'").Scan(&got); err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	if got != want {
		t.Fatalf("bookings = %d, want %d", got, want)
	}
}

type fakeTelegramClient struct {
	mu       sync.Mutex
	messages []telegram.SendMessageRequest
	photos   []telegram.SendPhotoRequest
}

func (f *fakeTelegramClient) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegram.Update, error) {
	return nil, nil
}

func (f *fakeTelegramClient) SendMessage(ctx context.Context, reqBody telegram.SendMessageRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, reqBody)
	return nil
}

func (f *fakeTelegramClient) SendPhoto(ctx context.Context, reqBody telegram.SendPhotoRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.photos = append(f.photos, reqBody)
	return nil
}

func (f *fakeTelegramClient) AnswerCallbackQuery(ctx context.Context, reqBody telegram.AnswerCallbackQueryRequest) error {
	return nil
}

func (f *fakeTelegramClient) GetFile(context.Context, string) (telegram.File, error) {
	return telegram.File{FilePath: "test/file", FileSize: 10}, nil
}

func (f *fakeTelegramClient) DownloadFile(context.Context, string, int64) ([]byte, error) {
	return []byte("test media"), nil
}

func (f *fakeTelegramClient) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.messages))
	for _, message := range f.messages {
		out = append(out, message.Text)
	}
	return out
}

func openAppStorePostgresContainer(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	if appStoreIntegrationDB == nil {
		t.Fatal("postgres integration db is not initialized")
	}
	resetAppStorePostgresDB(t, ctx, appStoreIntegrationDB)
	return appStoreIntegrationDB
}

func startAppStorePostgresContainer(ctx context.Context) (testcontainers.Container, *sql.DB, error) {
	if dsn := os.Getenv("INTEGRATION_DATABASE_URL"); dsn != "" {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("open integration db: %w", err)
		}
		if err := pingAppStorePostgres(ctx, db); err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		return nil, db, nil
	}
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "timetable",
			"POSTGRES_PASSWORD": "timetable",
			"POSTGRES_DB":       "timetable",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(context.Background())
		return nil, nil, fmt.Errorf("container host: %w", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		_ = container.Terminate(context.Background())
		return nil, nil, fmt.Errorf("container port: %w", err)
	}
	dsn := fmt.Sprintf("postgres://timetable:timetable@%s:%s/timetable?sslmode=disable", host, port.Port())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		_ = container.Terminate(context.Background())
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	if err := pingAppStorePostgres(ctx, db); err != nil {
		_ = db.Close()
		_ = container.Terminate(context.Background())
		return nil, nil, err
	}
	return container, db, nil
}

func pingAppStorePostgres(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("ping db: %w", lastErr)
}

func resetAppStorePostgresDB(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
TRUNCATE TABLE reminders, bookings, schedule_slots, admin_settings, admin_services, admin_profiles, users
RESTART IDENTITY CASCADE;
`)
	if err != nil {
		t.Fatalf("reset postgres db: %v", err)
	}
}
