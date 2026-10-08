package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Prune removes terminal actions older than MaxAge and then keeps only the newest MaxTerminal.
// Unfinished attempts are never pruned. Related steps are deleted by the foreign-key cascade.
func (s *HistoryStore) Prune(ctx context.Context, now time.Time) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("store: prune history canceled: %w", err)
	}
	if now.IsZero() {
		return 0, errors.New("store: prune time is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin history pruning: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var deleted int64
	cutoff := tsHistory(now.Add(-s.policy.MaxAge))
	res, err := tx.ExecContext(ctx, `DELETE FROM actions WHERE finished_at IS NOT NULL AND finished_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: prune expired actions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: count expired actions: %w", err)
	}
	deleted += n
	res, err = tx.ExecContext(ctx, `DELETE FROM actions WHERE id IN (
		SELECT id FROM actions WHERE finished_at IS NOT NULL ORDER BY started_at DESC,id DESC LIMIT -1 OFFSET ?
	)`, s.policy.MaxTerminal)
	if err != nil {
		return 0, fmt.Errorf("store: prune excess terminal actions: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: count excess terminal actions: %w", err)
	}
	deleted += n
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit history pruning: %w", err)
	}
	return deleted, nil
}
