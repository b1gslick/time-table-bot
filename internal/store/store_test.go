package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"

	"time-table-bot/internal/domain"
)

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "tim1106", want: "tim1106"},
		{in: "@tim1106", want: "tim1106"},
		{in: "  @Tim1106  ", want: "tim1106"},
		{in: "User_Name", want: "user_name"},
		{in: "   ", want: ""},
	}
	for _, tt := range tests {
		if got := normalizeUsername(tt.in); got != tt.want {
			t.Fatalf("normalizeUsername(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBootstrapSuperAdmin_UsesNormalizedUsernameAndRole(t *testing.T) {
	rec := newExecRecorder()
	db := openRecorderDB(t, rec)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSQLiteStore(db)
	if err := s.BootstrapSuperAdmin(context.Background()); err != nil {
		t.Fatalf("BootstrapSuperAdmin error: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(rec.calls))
	}
	call := rec.calls[0]
	if !strings.Contains(call.query, "ON CONFLICT(username)") {
		t.Fatalf("expected ON CONFLICT(username) in query, got: %s", call.query)
	}
	if !strings.Contains(call.query, "$1") || !strings.Contains(call.query, "$2") {
		t.Fatalf("expected postgres placeholders in query, got: %s", call.query)
	}
	if len(call.args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(call.args))
	}
	if got, ok := call.args[0].(string); !ok || got != "tim1106" {
		t.Fatalf("expected normalized username arg tim1106, got %#v", call.args[0])
	}
	if got, ok := call.args[1].(domain.Role); !ok || got != domain.RoleSuperAdmin {
		t.Fatalf("expected role arg super_admin, got %#v", call.args[1])
	}
}

func TestApplySchema_ExecutesEmbeddedSchema(t *testing.T) {
	rec := newExecRecorder()
	db := openRecorderDB(t, rec)
	t.Cleanup(func() { _ = db.Close() })

	s := NewSQLiteStore(db)
	if err := s.ApplySchema(context.Background()); err != nil {
		t.Fatalf("ApplySchema error: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected single schema exec call, got %d", len(rec.calls))
	}
	if strings.TrimSpace(rec.calls[0].query) != strings.TrimSpace(schemaSQL) {
		t.Fatal("executed schema SQL differs from embedded schema")
	}
}

type execCall struct {
	query string
	args  []any
}

type execRecorder struct {
	mu    sync.Mutex
	calls []execCall
}

func newExecRecorder() *execRecorder {
	return &execRecorder{}
}

func (r *execRecorder) addCall(query string, args []driver.NamedValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	flat := make([]any, 0, len(args))
	for _, a := range args {
		flat = append(flat, a.Value)
	}
	r.calls = append(r.calls, execCall{query: query, args: flat})
}

type recorderDriver struct {
	rec *execRecorder
}

func (d *recorderDriver) Open(name string) (driver.Conn, error) {
	return &recorderConn{rec: d.rec}, nil
}

type recorderConn struct {
	rec *execRecorder
}

func (c *recorderConn) Prepare(query string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

func (c *recorderConn) Close() error {
	return nil
}

func (c *recorderConn) Begin() (driver.Tx, error) {
	return &noopTx{}, nil
}

func (c *recorderConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.rec.addCall(query, args)
	return driver.RowsAffected(1), nil
}

func (c *recorderConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return nil, driver.ErrSkip
}

func (c *recorderConn) CheckNamedValue(_ *driver.NamedValue) error {
	return nil
}

type noopTx struct{}

func (t *noopTx) Commit() error {
	return nil
}

func (t *noopTx) Rollback() error {
	return nil
}

type noopRows struct{}

func (r *noopRows) Columns() []string {
	return nil
}

func (r *noopRows) Close() error {
	return nil
}

func (r *noopRows) Next(_ []driver.Value) error {
	return io.EOF
}

var registerRecorderDriverOnce sync.Once

func openRecorderDB(t *testing.T, rec *execRecorder) *sql.DB {
	t.Helper()
	const driverName = "store_exec_recorder"
	registerRecorderDriverOnce.Do(func() {
		sql.Register(driverName, &recorderDriver{})
	})

	drv := sql.Drivers()
	found := false
	for _, d := range drv {
		if d == driverName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("driver %s not registered", driverName)
	}

	// Re-register recorder pointer on each open by replacing global singleton in the driver value.
	// sql.Register cannot be called twice with same name, so use DSN lookup via package var.
	globalRecorder.mu.Lock()
	globalRecorder.rec = rec
	globalRecorder.mu.Unlock()

	return sql.OpenDB(&recorderConnector{})
}

type recorderConnector struct{}

func (c *recorderConnector) Connect(_ context.Context) (driver.Conn, error) {
	globalRecorder.mu.Lock()
	rec := globalRecorder.rec
	globalRecorder.mu.Unlock()
	if rec == nil {
		rec = newExecRecorder()
	}
	return &recorderConn{rec: rec}, nil
}

func (c *recorderConnector) Driver() driver.Driver {
	return &recorderDriver{}
}

var globalRecorder struct {
	mu  sync.Mutex
	rec *execRecorder
}
