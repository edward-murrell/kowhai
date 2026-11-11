// Package store holds the sqlc-generated queries, the embedded migrations
// and the connection set-up for SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"runtime"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB holds two pools on one SQLite file. Writes go through a pool with a
// single connection, so there is only ever one writer and SQLITE_BUSY
// errors cannot happen between our own connections.
type DB struct {
	Write *sql.DB
	Read  *sql.DB
	// R runs read queries on the read pool.
	R *Queries
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	q.Set("_time_format", "sqlite")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens the database, runs migrations and returns both pools.
func Open(ctx context.Context, path string) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := Migrate(ctx, w); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	n := max(4, runtime.NumCPU())
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	return &DB{Write: w, Read: r, R: New(r)}, nil
}

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (db *DB) Close() error {
	return errors.Join(db.Read.Close(), db.Write.Close())
}

// Tx is a write transaction. Hooks added with AfterCommit run once the
// transaction commits.
type Tx struct {
	*Queries
	Conn        *sql.Tx
	afterCommit []func()
}

// AfterCommit registers fn to run after a successful commit.
func (t *Tx) AfterCommit(fn func()) { t.afterCommit = append(t.afterCommit, fn) }

// WriteTx runs fn in a write transaction on the single write connection.
func (db *DB) WriteTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := db.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	tx := &Tx{Queries: New(sqlTx), Conn: sqlTx}
	if err := fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return err
	}
	for _, f := range tx.afterCommit {
		f()
	}
	return nil
}

// Ping checks both pools.
func (db *DB) Ping(ctx context.Context) error {
	return errors.Join(db.Write.PingContext(ctx), db.Read.PingContext(ctx))
}

// Snapshot writes a consistent copy of the database to path.
func (db *DB) Snapshot(ctx context.Context, path string) error {
	_, err := db.Write.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}
