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

//go:embed schema_interview_templates.sql
var schemaInterviewTemplates string

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
	// Шаблоны интервью: общая таблица (оба диалекта) + колонка sessions.template_id.
	for _, stmt := range strings.Split(schemaInterviewTemplates, ";\n") {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "ALTER TABLE") {
			// SQLite: ADD COLUMN (идемпотентность — проверка колонки); Postgres: IF NOT EXISTS.
			col := "template_id"
			if strings.Contains(s, "program") {
				col = "program"
			}
			if d == DialectPostgres {
				s = strings.Replace(s, "ADD COLUMN "+col, "ADD COLUMN IF NOT EXISTS "+col, 1)
				s = strings.Replace(s, "INTEGER REFERENCES", "BIGINT REFERENCES", 1)
			} else if columnExists(ctx, dbx, "sessions", col, d) {
				continue
			}
		}
		if _, err := dbx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate interview_templates: %w", err)
		}
	}
	return nil
}

// columnExists — есть ли колонка в таблице (SQLite: PRAGMA, Postgres: information_schema).
func columnExists(ctx context.Context, dbx *sql.DB, table, col string, d Dialect) bool {
	if d == DialectSQLite {
		rows, err := dbx.QueryContext(ctx, "PRAGMA table_info("+table+")")
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return false
			}
			if name == col {
				return true
			}
		}
		return false
	}
	var n int
	err := dbx.QueryRowContext(ctx,
		"SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2", table, col).Scan(&n)
	return err == nil
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
