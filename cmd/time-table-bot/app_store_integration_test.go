//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
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

	steps := []string{
		"/start",
		"Русский",
		"1",
		"1",
		"1",
		"Нет",
		"Ближайшее время",
		"1",
	}
	for _, text := range steps {
		if err := bookingBot.HandleMessage(ctx, &telegram.Message{
			From: client,
			Chat: chat,
			Text: text,
		}); err != nil {
			t.Fatalf("HandleMessage(%q): %v", text, err)
		}
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
