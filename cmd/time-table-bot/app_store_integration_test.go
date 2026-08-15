//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
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
		"cat:1",
		"sub:1",
		"svc:1",
		"more:no",
		"time:nearest",
		"slot:1",
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
