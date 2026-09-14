package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrWhiteboardNotFound — холст сессии ещё не сохранялся.
var ErrWhiteboardNotFound = errors.New("whiteboard not found")

// WhiteboardStore — состояния холста System Design (ADR-004, WP-10).
type WhiteboardStore struct {
	db *sql.DB
	d  Dialect
}

// NewWhiteboardStore создаёт хранилище.
func NewWhiteboardStore(dbx *sql.DB, d Dialect) *WhiteboardStore {
	return &WhiteboardStore{db: dbx, d: d}
}

// Save — upsert холста сессии (state — JSON Excalidraw, blocks — JSON структуры).
func (s *WhiteboardStore) Save(ctx context.Context, sessionID int64, state, blocks, pngPath []byte) error {
	var png any
	if len(pngPath) > 0 {
		png = string(pngPath)
	}
	_, err := s.db.ExecContext(ctx, s.d.q(`
		INSERT INTO whiteboards (session_id, state, blocks, png_path, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (session_id) DO UPDATE SET
			state = excluded.state,
			blocks = excluded.blocks,
			png_path = excluded.png_path,
			updated_at = excluded.updated_at`),
		sessionID, string(state), string(blocks), png, nowRFC3339())
	if err != nil {
		return fmt.Errorf("save whiteboard: %w", err)
	}
	return nil
}

// Get — холст сессии (ErrWhiteboardNotFound, если не сохранялся).
func (s *WhiteboardStore) Get(ctx context.Context, sessionID int64) (state, blocks, pngPath []byte, err error) {
	var sState, sBlocks string
	var sPng sql.NullString
	err = s.db.QueryRowContext(ctx, s.d.q(`
		SELECT state, blocks, png_path FROM whiteboards WHERE session_id = ?`), sessionID).
		Scan(&sState, &sBlocks, &sPng)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, ErrWhiteboardNotFound
		}
		return nil, nil, nil, fmt.Errorf("get whiteboard: %w", err)
	}
	if sPng.Valid {
		pngPath = []byte(sPng.String)
	}
	return []byte(sState), []byte(sBlocks), pngPath, nil
}
