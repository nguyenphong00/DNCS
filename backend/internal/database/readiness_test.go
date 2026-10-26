package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type readinessDatabaseStub struct {
	pingErr  error
	queryErr error
	ready    bool
	query    string
	args     []any
}

func (database *readinessDatabaseStub) Ping(context.Context) error {
	return database.pingErr
}

func (database *readinessDatabaseStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	database.query = query
	database.args = args
	return readinessRowStub{ready: database.ready, err: database.queryErr}
}

type readinessRowStub struct {
	ready bool
	err   error
}

func (row readinessRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*bool) = row.ready
	return nil
}

func TestCheckReadinessRequiresMigration202610090020(t *testing.T) {
	database := &readinessDatabaseStub{}
	err := CheckReadiness(context.Background(), database)
	if !errors.Is(err, ErrSchemaNotReady) {
		t.Fatalf("missing schema migration error = %v", err)
	}
	if !strings.Contains(err.Error(), "202610090020") {
		t.Fatalf("missing required migration in error: %v", err)
	}
	if len(database.args) != 1 || database.args[0] != requiredSchemaBaseline {
		t.Fatalf("unexpected schema baseline query args: %#v", database.args)
	}
}

func TestCheckReadinessAcceptsRequiredSchemaBaseline(t *testing.T) {
	database := &readinessDatabaseStub{ready: true}
	if err := CheckReadiness(context.Background(), database); err != nil {
		t.Fatalf("ready database rejected: %v", err)
	}
}

func TestCheckReadinessRejectsDatabaseFailure(t *testing.T) {
	pingErr := errors.New("database unavailable")
	database := &readinessDatabaseStub{pingErr: pingErr}
	err := CheckReadiness(context.Background(), database)
	if !errors.Is(err, pingErr) {
		t.Fatalf("database error = %v", err)
	}
	if database.query != "" {
		t.Fatal("schema was queried after ping failed")
	}
}

func TestCheckReadinessRejectsSchemaQueryFailure(t *testing.T) {
	queryErr := errors.New("catalog unavailable")
	database := &readinessDatabaseStub{ready: true, queryErr: queryErr}
	err := CheckReadiness(context.Background(), database)
	if !errors.Is(err, queryErr) {
		t.Fatalf("schema query error = %v", err)
	}
}
