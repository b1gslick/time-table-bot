//go:build integration

package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"time-table-bot/internal/domain"
)

func TestPostgresStoreIntegration_BookingFlow(t *testing.T) {
	ctx := context.Background()
	db := openPostgresContainer(t, ctx)
	repo := NewPostgresStore(db)

	if err := repo.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := repo.BootstrapSuperAdmin(ctx, "tim1106"); err != nil {
		t.Fatalf("BootstrapSuperAdmin: %v", err)
	}

	super, err := repo.UpsertUser(ctx, 1001, "tim1106", "Super Admin")
	if err != nil {
		t.Fatalf("UpsertUser super: %v", err)
	}
	if super.Role != domain.RoleSuperAdmin {
		t.Fatalf("super role = %s, want %s", super.Role, domain.RoleSuperAdmin)
	}

	admin, err := repo.UpsertUser(ctx, 2001, "master", "Master")
	if err != nil {
		t.Fatalf("UpsertUser admin: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", domain.RoleAdmin, admin.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	user, err := repo.UpsertUser(ctx, 3001, "client", "Client")
	if err != nil {
		t.Fatalf("UpsertUser client: %v", err)
	}

	start := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
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

	slots, err := repo.ListAvailableSlots(ctx, admin.ID, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListAvailableSlots before booking: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("available slots before booking = %d, want 1", len(slots))
	}

	booking, err := repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slot.ID,
		UserID:        &user.ID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: 45,
	})
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	if booking.TravelMinutes != 45 {
		t.Fatalf("booking travel = %d, want 45", booking.TravelMinutes)
	}

	slots, err = repo.ListAvailableSlots(ctx, admin.ID, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListAvailableSlots after booking: %v", err)
	}
	if len(slots) != 0 {
		t.Fatalf("available slots after booking = %d, want 0", len(slots))
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO reminders (booking_id, chat_id, kind, recipient_role, send_at, payload)
VALUES ($1, $2, 'day_before', 'user', $3, 'test reminder')
`, booking.ID, 3001, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}

	reminders, err := repo.ListRemindersToSend(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("ListRemindersToSend: %v", err)
	}
	if len(reminders) != 1 || reminders[0].ChatID != 3001 {
		t.Fatalf("reminders = %#v, want one reminder for chat 3001", reminders)
	}
	if err := repo.MarkReminderSent(ctx, reminders[0].ID, time.Now()); err != nil {
		t.Fatalf("MarkReminderSent: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO reminders (booking_id, chat_id, kind, recipient_role, send_at, payload)
VALUES ($1, $2, 'hour_before', 'user', $3, 'cancelled booking reminder')
`, booking.ID, 3001, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("insert cancellation reminder: %v", err)
	}
	if err := repo.DeleteBooking(ctx, booking.ID, "test_cancel"); err != nil {
		t.Fatalf("DeleteBooking: %v", err)
	}
	reminders, err = repo.ListRemindersToSend(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("ListRemindersToSend after cancellation: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("reminders after cancellation = %#v, want none", reminders)
	}
	var pending int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM reminders
WHERE booking_id = $1 AND sent_at IS NULL;
`, booking.ID).Scan(&pending); err != nil {
		t.Fatalf("count pending reminders after cancellation: %v", err)
	}
	if pending != 0 {
		t.Fatalf("pending reminders after cancellation = %d, want 0", pending)
	}
}

func openPostgresContainer(t *testing.T, ctx context.Context) *sql.DB {
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
