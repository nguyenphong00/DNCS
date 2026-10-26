package pgutil

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func Error(err error) (*pgconn.PgError, bool) {
	var databaseError *pgconn.PgError
	ok := errors.As(err, &databaseError)
	return databaseError, ok
}

func Constraint(err error) string {
	if databaseError, ok := Error(err); ok {
		return databaseError.ConstraintName
	}
	return ""
}

func SQLState(err error) string {
	if databaseError, ok := Error(err); ok {
		return databaseError.Code
	}
	return ""
}
