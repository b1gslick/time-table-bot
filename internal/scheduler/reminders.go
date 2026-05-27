package scheduler

import (
	"context"
	"log"
	"time"
)

type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type Reminder struct {
	ID     int64
	ChatID int64
	Text   string
}

type Store interface {
	PrepareUpcomingReminders(ctx context.Context, now time.Time) error
	DueReminders(ctx context.Context, now time.Time, limit int) ([]Reminder, error)
	MarkReminderSent(ctx context.Context, reminderID int64, sentAt time.Time) error
}

type Service struct {
	store  Store
	sender Sender
	loc    *time.Location
	logger *log.Logger
}

func New(store Store, sender Sender, loc *time.Location, logger *log.Logger) *Service {
	return &Service{
		store:  store,
		sender: sender,
		loc:    loc,
		logger: logger,
	}
}

func (s *Service) Start(ctx context.Context) {
	s.runTick(ctx)

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runTick(ctx)
		}
	}
}

func (s *Service) runTick(ctx context.Context) {
	now := time.Now().In(s.loc)

	if err := s.store.PrepareUpcomingReminders(ctx, now); err != nil {
		s.logger.Printf("scheduler: prepare reminders failed: %v", err)
	}

	reminders, err := s.store.DueReminders(ctx, now, 100)
	if err != nil {
		s.logger.Printf("scheduler: due reminders query failed: %v", err)
		return
	}

	for _, reminder := range reminders {
		if err := s.sender.SendMessage(ctx, reminder.ChatID, reminder.Text); err != nil {
			s.logger.Printf("scheduler: send reminder #%d failed: %v", reminder.ID, err)
			continue
		}

		if err := s.store.MarkReminderSent(ctx, reminder.ID, now); err != nil {
			s.logger.Printf("scheduler: mark reminder #%d sent failed: %v", reminder.ID, err)
		}
	}
}
