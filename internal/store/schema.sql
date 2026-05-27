CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT UNIQUE,
    username TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('super_admin', 'admin', 'user')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

CREATE TABLE IF NOT EXISTS admin_profiles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    booking_notice INTEGER NOT NULL DEFAULT 60 CHECK (booking_notice >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admin_services (
    id BIGSERIAL PRIMARY KEY,
    admin_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    duration_min INTEGER NOT NULL CHECK (duration_min > 0),
    price_cents BIGINT NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (admin_user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_admin_services_admin_active
    ON admin_services(admin_user_id, is_active);

CREATE TABLE IF NOT EXISTS admin_settings (
    admin_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (admin_user_id, key)
);

CREATE TABLE IF NOT EXISTS schedule_slots (
    id BIGSERIAL PRIMARY KEY,
    admin_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service_id BIGINT REFERENCES admin_services(id) ON DELETE SET NULL,
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,
    capacity INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0),
    status TEXT NOT NULL CHECK (status IN ('open', 'closed')),
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_at > start_at)
);

CREATE INDEX IF NOT EXISTS idx_schedule_slots_admin_time
    ON schedule_slots(admin_user_id, start_at, end_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_slots_admin_start_unique
    ON schedule_slots(admin_user_id, start_at);
CREATE INDEX IF NOT EXISTS idx_schedule_slots_status
    ON schedule_slots(status);

CREATE TABLE IF NOT EXISTS bookings (
    id BIGSERIAL PRIMARY KEY,
    slot_id BIGINT NOT NULL REFERENCES schedule_slots(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    service_id BIGINT REFERENCES admin_services(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('booked', 'cancelled', 'blocked')),
    travel_minutes INTEGER NOT NULL DEFAULT 30 CHECK (travel_minutes >= 0),
    note TEXT NOT NULL DEFAULT '',
    booked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bookings_slot_status
    ON bookings(slot_id, status);
CREATE INDEX IF NOT EXISTS idx_bookings_user_status
    ON bookings(user_id, status);

CREATE TABLE IF NOT EXISTS reminders (
    id BIGSERIAL PRIMARY KEY,
    booking_id BIGINT NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    chat_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    recipient_role TEXT NOT NULL,
    send_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ,
    channel TEXT NOT NULL DEFAULT 'telegram',
    payload TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (booking_id, kind, recipient_role, chat_id)
);

CREATE INDEX IF NOT EXISTS idx_reminders_due
    ON reminders(sent_at, send_at);
