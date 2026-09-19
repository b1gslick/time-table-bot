package main

import "testing"

func TestReminderActionKeyboard(t *testing.T) {
	kb := reminderActionKeyboard("Напоминание: у вас запись 20.09.2026 10:00.", 42)
	if kb == nil || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("keyboard = %#v, want one row with two buttons", kb)
	}
	if got := kb.InlineKeyboard[0][0].Text; got != "Иду" {
		t.Fatalf("go button text = %q, want Иду", got)
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "remindergo:42" {
		t.Fatalf("go callback = %q, want remindergo:42", got)
	}
	if got := kb.InlineKeyboard[0][1].Text; got != "Отказаться" {
		t.Fatalf("cancel button text = %q, want Отказаться", got)
	}
	if got := kb.InlineKeyboard[0][1].CallbackData; got != "remindercancel:42" {
		t.Fatalf("cancel callback = %q, want remindercancel:42", got)
	}

	en := reminderActionKeyboard("Reminder: you have a booking on 20.09.2026 10:00.", 42)
	if got := en.InlineKeyboard[0][0].Text; got != "I'm coming" {
		t.Fatalf("english go button text = %q, want I'm coming", got)
	}
	if got := en.InlineKeyboard[0][1].Text; got != "Cancel" {
		t.Fatalf("english cancel button text = %q, want Cancel", got)
	}
}
