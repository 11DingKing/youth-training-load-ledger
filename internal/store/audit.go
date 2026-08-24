package store

import (
	"context"
	"fmt"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func (t *Tx) InsertAudit(ctx context.Context, event domain.AuditEvent) (domain.AuditEvent, error) {
	if err := event.Validate(); err != nil {
		return domain.AuditEvent{}, err
	}
	result, err := t.tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id, action, object_type, object_id,
        outcome, basis, request_id, metadata_json, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.Outcome, event.Basis,
		event.RequestID, event.Metadata, event.CreatedAt)
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("insert audit event: %w", err)
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return domain.AuditEvent{}, fmt.Errorf("read audit id: %w", err)
	}
	return event, nil
}

func (s *Store) ListAudit(ctx context.Context, objectType string, objectID int64, limit, offset int) ([]domain.AuditEvent, error) {
	if objectType == "" || objectID <= 0 || limit < 1 || limit > 100 || offset < 0 {
		return nil, domain.FieldError{Field: "audit_query", Problem: "invalid object or pagination"}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor_id, action, object_type, object_id, outcome, basis,
        request_id, metadata_json, created_at FROM audit_events WHERE object_type = ? AND object_id = ?
        ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, objectType, objectID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var event domain.AuditEvent
		if err = rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.ObjectType, &event.ObjectID,
			&event.Outcome, &event.Basis, &event.RequestID, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		items = append(items, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return items, nil
}
