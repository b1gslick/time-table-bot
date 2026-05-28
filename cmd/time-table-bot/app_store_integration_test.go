//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/domain"
	"time-table-bot/internal/store"
)

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

	start := time.Date(2026, 6, 1, 10, 0, 0, 0, loc)
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

	bookedStart, err := app.BookForUserByIndex(ctx, 3001, 1, 30)
	if err != nil {
		t.Fatalf("BookForUserByIndex: %v", err)
	}
	if !bookedStart.Equal(start) {
		t.Fatalf("booked start = %s, want %s", bookedStart, start)
	}

	free, err = app.ListFreeSlotsForServices(ctx, 3001, []int{1, 2}, start)
	if err != nil {
		t.Fatalf("ListFreeSlotsForServices after booking: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("free slots after booking = %d, want 0", len(free))
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

	result, err := app.GenerateSchedule(ctx, 2001, bot.GenerateScheduleRequest{
		Month:  time.Date(2026, 6, 1, 0, 0, 0, 0, loc),
		Months: 2,
	})
	if err != nil {
		t.Fatalf("GenerateSchedule: %v", err)
	}
	if result.Created == 0 {
		t.Fatalf("created slots = 0, want slots for two months")
	}

	var juneSlots, julySlots int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3
`, admin.ID, time.Date(2026, 6, 1, 0, 0, 0, 0, loc), time.Date(2026, 7, 1, 0, 0, 0, 0, loc)).Scan(&juneSlots); err != nil {
		t.Fatalf("count june slots: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3
`, admin.ID, time.Date(2026, 7, 1, 0, 0, 0, 0, loc), time.Date(2026, 8, 1, 0, 0, 0, 0, loc)).Scan(&julySlots); err != nil {
		t.Fatalf("count july slots: %v", err)
	}
	if juneSlots == 0 || julySlots == 0 {
		t.Fatalf("juneSlots=%d julySlots=%d, want both months filled", juneSlots, julySlots)
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

func openAppStorePostgresContainer(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
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
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	dsn := fmt.Sprintf("postgres://timetable:timetable@%s:%s/timetable?sslmode=disable", host, port.Port())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	return db
}
