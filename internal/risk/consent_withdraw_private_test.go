package risk_test

import (
	"testing"

	"github.com/11DingKing/youth-training-load-ledger/internal/audit"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

func TestFailedConsentWithdrawalPreservesEligibility(t *testing.T) {
	f := newWorkflowFixture(t)
	f.prepareActiveAthlete()
	ctx := audit.WithRequestID(t.Context(), "consent-withdraw-rollback")

	if _, err := f.database.DB().ExecContext(ctx, `CREATE TRIGGER reject_withdraw_transition
		BEFORE INSERT ON status_transitions
		WHEN NEW.reason LIKE 'guardian consent withdrawn:%'
		BEGIN SELECT RAISE(ABORT, 'transition storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.profiles.WithdrawConsent(ctx, f.guardian, f.athlete.ID, "summer schedule changed"); err == nil {
		t.Fatal("withdrawal succeeded while transition storage was unavailable")
	}

	athlete, err := f.database.AthleteByID(ctx, f.athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	consent, err := f.database.CurrentConsent(ctx, f.athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if athlete.Status != domain.AthleteActive || consent.Status != domain.ConsentGranted {
		t.Fatalf("failed withdrawal changed eligibility: athlete=%s consent=%s", athlete.Status, consent.Status)
	}

	if _, err = f.database.DB().ExecContext(ctx, `DROP TRIGGER reject_withdraw_transition`); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := f.profiles.WithdrawConsent(ctx, f.guardian, f.athlete.ID, "summer schedule changed")
	if err != nil {
		t.Fatal(err)
	}
	consent, err = f.database.CurrentConsent(ctx, f.athlete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.Status != domain.AthletePaused || consent.Status != domain.ConsentWithdrawn {
		t.Fatalf("successful withdrawal incomplete: athlete=%s consent=%s", withdrawn.Status, consent.Status)
	}
}
