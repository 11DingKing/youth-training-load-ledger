package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/migrate"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	database, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testUser(role domain.Role, email string, now time.Time) domain.User {
	return domain.User{Email: email, DisplayName: email, Role: role, PasswordHash: "test-hash", CreatedAt: now}
}

func seedRelationships(t *testing.T, database *Store, now time.Time) (student, guardian, coach, advisor domain.User, athlete domain.Athlete) {
	t.Helper()
	var err error
	student, err = database.CreateUser(t.Context(), testUser(domain.RoleStudent, "student@store.test", now))
	if err != nil {
		t.Fatal(err)
	}
	guardian, err = database.CreateUser(t.Context(), testUser(domain.RoleGuardian, "guardian@store.test", now))
	if err != nil {
		t.Fatal(err)
	}
	coach, err = database.CreateUser(t.Context(), testUser(domain.RoleCoach, "coach@store.test", now))
	if err != nil {
		t.Fatal(err)
	}
	advisor, err = database.CreateUser(t.Context(), testUser(domain.RoleAdvisor, "advisor@store.test", now))
	if err != nil {
		t.Fatal(err)
	}
	err = database.WithTx(t.Context(), func(tx *Tx) error {
		athlete, err = tx.CreateAthlete(t.Context(), domain.Athlete{
			StudentUserID: student.ID, GuardianUserID: guardian.ID, CoachUserID: &coach.ID,
			AdvisorUserID: &advisor.ID, BirthDate: now.AddDate(-14, 0, 0), Timezone: "UTC",
			Status: domain.AthleteActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestOpenAppliesAllMigrationsAndForeignKeys(t *testing.T) {
	database := openTestStore(t)
	version, err := migrate.CurrentVersion(t.Context(), database.DB())
	if err != nil || version != len(migrate.All) {
		t.Fatalf("migration version = %d, %v", version, err)
	}
	var foreignKeys int
	if err = database.DB().QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d", foreignKeys)
	}
	if err = database.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationIsIdempotentAndDetectsNameConflict(t *testing.T) {
	database := openTestStore(t)
	if err := migrate.Apply(t.Context(), database.DB()); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if _, err := database.DB().ExecContext(t.Context(), `UPDATE schema_migrations SET name = 'tampered' WHERE version = 2`); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Apply(t.Context(), database.DB()); err == nil {
		t.Fatal("migration name conflict was accepted")
	}
}

func TestTransactionRollsBackAllWrites(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	sentinel := errors.New("audit sink unavailable")
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		if _, err := tx.CreateUser(t.Context(), testUser(domain.RoleStudent, "rollback@store.test", now)); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v", err)
	}
	if _, err = database.UserByEmail(t.Context(), "rollback@store.test"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rolled-back user lookup = %v", err)
	}
}

func TestTransactionHonorsCanceledContext(t *testing.T) {
	database := openTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := database.WithTx(ctx, func(*Tx) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("WithTx canceled = %v, called=%t", err, called)
	}
}

func TestUserEmailUniquenessAndSessionRevocation(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	user, err := database.CreateUser(t.Context(), testUser(domain.RoleGuardian, "Unique@Store.Test", now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.CreateUser(t.Context(), testUser(domain.RoleGuardian, "unique@store.test", now)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate user error = %v", err)
	}
	session, err := database.CreateSession(t.Context(), domain.Session{UserID: user.ID, TokenHash: "digest", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil || session.ID == 0 {
		t.Fatalf("session = %+v, %v", session, err)
	}
	gotSession, gotUser, err := database.SessionUser(t.Context(), "digest")
	if err != nil || gotSession.ID != session.ID || gotUser.ID != user.ID {
		t.Fatalf("SessionUser = %+v %+v %v", gotSession, gotUser, err)
	}
	if err = database.RevokeSession(t.Context(), "digest", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	gotSession, _, err = database.SessionUser(t.Context(), "digest")
	if err != nil || gotSession.RevokedAt == nil {
		t.Fatalf("revoked session = %+v, %v", gotSession, err)
	}
}

func TestForeignKeyRejectsUnknownRelationship(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		_, err := tx.CreateAthlete(t.Context(), domain.Athlete{StudentUserID: 901, GuardianUserID: 902,
			BirthDate: now.AddDate(-14, 0, 0), Timezone: "UTC", Status: domain.AthleteDraft,
			Version: 1, CreatedAt: now, UpdatedAt: now})
		return err
	})
	if err == nil {
		t.Fatal("unknown user relationship was persisted")
	}
}

func TestAthleteOptimisticUpdateDetectsStaleVersion(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	_, _, _, _, athlete := seedRelationships(t, database, now)
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		current, err := tx.AthleteByID(t.Context(), athlete.ID)
		if err != nil {
			return err
		}
		current.Status = domain.AthletePaused
		current.PausedReason = "first update"
		current.Version++
		if err = tx.UpdateAthlete(t.Context(), current, 1); err != nil {
			return err
		}
		current.PausedReason = "stale overwrite"
		current.Version++
		return tx.UpdateAthlete(t.Context(), current, 1)
	})
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	got, err := database.AthleteByID(t.Context(), athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Status != domain.AthleteActive {
		t.Fatalf("transaction with stale update partially committed: %+v", got)
	}
}

func TestActivityIdempotencyUniqueWithinAthlete(t *testing.T) {
	database := openTestStore(t)
	now := time.Now().UTC()
	_, _, coach, _, athlete := seedRelationships(t, database, now)
	err := database.WithTx(t.Context(), func(tx *Tx) error {
		baseline, err := tx.CreateBaseline(t.Context(), domain.BaselineAssessment{AthleteID: athlete.ID,
			AssessorUserID: coach.ID, EnduranceScore: 50, StrengthScore: 50, MobilityScore: 50,
			RestingHeartRate: 65, Conclusion: "baseline", AssessedAt: now})
		if err != nil || baseline.ID == 0 {
			return err
		}
		prescription, err := tx.CreatePrescription(t.Context(), domain.Prescription{AthleteID: athlete.ID,
			AuthorUserID: coach.ID, WeekStart: now, Version: 1, Status: domain.PrescriptionPublished,
			WeeklyLoadLimit: 1000, MaxSessionLoad: 500, MinRecoveryHours: 24, StrengthDays: 2,
			Basis: "baseline", PublishedAt: &now, CreatedAt: now})
		if err != nil {
			return err
		}
		item := domain.ActivityLog{AthleteID: athlete.ID, PrescriptionID: prescription.ID,
			RecorderUserID: athlete.StudentUserID, IdempotencyKey: "same-key", OccurredAt: now,
			RecordedAt: now, DurationMinutes: 30, PerceivedEffort: 5, LoadUnits: 150,
			Source: domain.ActivityStudent, CreatedAt: now}
		if _, err = tx.CreateActivity(t.Context(), item); err != nil {
			return err
		}
		_, err = tx.CreateActivity(t.Context(), item)
		return err
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate activity error = %v", err)
	}
	var count int
	if err = database.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM activity_logs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed transaction retained %d activities", count)
	}
}

func TestPersistentStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := t.Context()
	now := time.Now().UTC()
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.CreateUser(ctx, testUser(domain.RoleAdvisor, "restart@store.test", now))
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	loaded, err := second.UserByEmail(ctx, "restart@store.test")
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("loaded after restart = %+v, %v", loaded, err)
	}
	if err = second.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}
