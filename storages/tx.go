package storages

import (
	"context"
	"database/sql"
)

type Tx interface {
	Commit() error
	Rollback() error
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) (*sql.Row, error)
}
