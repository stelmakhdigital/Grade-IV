// Package db — подключение к БД (sqlite в dev / postgres в prod) и миграция схемы.
// Драйверы: modernc.org/sqlite (чистый Go, без cgo) и pgx/v5 (PostgreSQL).
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed schema_sqlite.sql
var schemaSQLite string

//go:embed schema_postgres.sql
var schemaPostgres string

// Dialect — диалект СУБД.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// Open открывает БД по DATABASE_URL:
//
//	sqlite:///path/to/file.db, sqlite://:memory:
//	postgres://user:pass@host:5432/dbname
func Open(databaseURL string) (*sql.DB, Dialect, error) {
	switch {
	case databaseURL == "":
		return nil, "", fmt.Errorf("DATABASE_URL не задан")

	case strings.HasPrefix(databaseURL, "sqlite://"):
		rest := strings.TrimPrefix(databaseURL, "sqlite://")
		var dsn string
		switch rest {
		case "", ":memory:":
			dsn = ":memory:"
		default:
			dsn = strings.TrimPrefix(rest, "/")
		}
		if dsn != ":memory:" {
			if dir := filepath.Dir(dsn); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, DialectSQLite, fmt.Errorf("создание каталога данных: %w", err)
				}
			}
			dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
		}
		conn, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, DialectSQLite, fmt.Errorf("open sqlite: %w", err)
		}
		if dsn == ":memory:" {
			conn.SetMaxOpenConns(1) // одна связь: in-memory БД общая для процесса
		}
		if err := conn.Ping(); err != nil {
			_ = conn.Close()
			return nil, DialectSQLite, fmt.Errorf("ping sqlite: %w", err)
		}
		return conn, DialectSQLite, nil

	case strings.HasPrefix(databaseURL, "postgres://"),
		strings.HasPrefix(databaseURL, "postgresql://"):
		conn, err := sql.Open("pgx", databaseURL)
		if err != nil {
			return nil, DialectPostgres, fmt.Errorf("open postgres: %w", err)
		}
		if err := conn.Ping(); err != nil {
			_ = conn.Close()
			return nil, DialectPostgres, fmt.Errorf("ping postgres: %w", err)
		}
		return conn, DialectPostgres, nil

	default:
		return nil, "", fmt.Errorf("неподдерживаемая схема DATABASE_URL: %q", databaseURL)
	}
}

// Migrate применяет схему (идемпотентно: CREATE ... IF NOT EXISTS).
func Migrate(ctx context.Context, dbx *sql.DB, d Dialect) error {
	var raw string
	switch d {
	case DialectSQLite:
		raw = schemaSQLite
	case DialectPostgres:
		raw = schemaPostgres
	default:
		return fmt.Errorf("неподдерживаемый диалект: %q", d)
	}
	for _, stmt := range strings.Split(raw, ";\n") {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		if _, err := dbx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// q переписывает плейсхолдеры ? → $1..$n для postgres.
// Ограничение: литеральный '?' не используется в тексте запросов этого пакета.
func (d Dialect) q(query string) string {
	if d != DialectPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}
