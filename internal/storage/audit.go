package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RecordAudit appends metadata for an authenticated administrative action.
func (db *DB) RecordAudit(ctx context.Context, actorUserID, action string, detail any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode action audit: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO action_audit(occurred_at, actor_user_id, action, detail_json) VALUES (?, ?, ?, ?)`, formatTime(time.Now().UTC()), actorUserID, action, string(encoded)); err != nil {
		return fmt.Errorf("record action audit: %w", err)
	}
	return nil
}
