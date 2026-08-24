package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestCanceledTransactionRollsBackBeforeCommit(t *testing.T) {
	database := openTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := database.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateUser(ctx, domain.User{Email: "canceled-tx@example.test",
			DisplayName: "Canceled", PasswordHash: "hash", Role: domain.RoleStudent,
			Active: true, CreatedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithTx error = %v, want context canceled", err)
	}
	var count int
	if err := database.DB().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM users WHERE email = ?", "canceled-tx@example.test").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled transaction committed %d users", count)
	}
}
