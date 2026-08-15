package infra_test

import (
	"context"
	"testing"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
	sharedquota "example.com/phan-quyen-golang/internal/shared/quota"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestQuotaReserveCommitReleaseAndIdempotency(t *testing.T) {
	database := testutil.Database(t)
	ctx := context.Background()
	cost := domain.QuotaCost{Subject: domain.Subject{Type: domain.SubjectUser, ID: "user-personal"}, QuotaKey: "membership_applications", Cost: 1}
	operation := sharedquota.Operation{ID: "quota-operation", ActorID: "user-personal", ResourceType: "membership", Action: "apply", ExpiresAt: time.Now().Add(time.Minute)}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sharedquota.ReserveQuotas(ctx, tx, []domain.QuotaCost{cost}, operation); err != nil {
		t.Fatal(err)
	}
	if err := sharedquota.CommitQuotas(ctx, tx, operation); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var used, reserved int64
	if err := database.QueryRow(`SELECT SUM(used),SUM(reserved) FROM subject_quota_entitlements_v2 WHERE subject_type='user' AND subject_id='user-personal' AND quota_key='membership_applications'`).Scan(&used, &reserved); err != nil {
		t.Fatal(err)
	}
	if used != 1 || reserved != 0 {
		t.Fatalf("used=%d reserved=%d", used, reserved)
	}
	retry, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sharedquota.ReserveQuotas(ctx, retry, []domain.QuotaCost{cost}, operation); err == nil {
		t.Fatal("duplicate idempotency operation was accepted")
	}
	_ = retry.Rollback()
}

func TestQuotaConsumptionAggregatesPlanAndAddonEntitlements(t *testing.T) {
	database := testutil.Database(t)
	if _, err := database.Exec(`INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,period_start,source_type,source_id) VALUES('personal-addon','user','user-personal','membership_applications','allow',1,'1970-01-01T00:00:00Z','addon','addon-1')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cost := domain.QuotaCost{Subject: domain.Subject{Type: domain.SubjectUser, ID: "user-personal"}, QuotaKey: "membership_applications", Cost: 4}
	operation := sharedquota.Operation{ID: "aggregate-operation", ActorID: "user-personal", ResourceType: "membership", Action: "apply", ExpiresAt: time.Now().Add(time.Minute)}
	if err := sharedquota.ConsumeQuotas(ctx, tx, []domain.QuotaCost{cost}, operation); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var used, reservations, commits int64
	if err := database.QueryRow(`SELECT SUM(used) FROM subject_quota_entitlements_v2 WHERE subject_type='user' AND subject_id='user-personal' AND quota_key='membership_applications'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM quota_reservations_v2 WHERE id='aggregate-operation'`).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM quota_ledger_v2 WHERE idempotency_key='aggregate-operation' AND operation='commit' AND amount=4`).Scan(&commits); err != nil {
		t.Fatal(err)
	}
	if used != 4 || reservations != 2 || commits != 1 {
		t.Fatalf("used=%d reservations=%d commits=%d", used, reservations, commits)
	}
}
