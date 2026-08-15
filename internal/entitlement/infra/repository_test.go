package infra_test

import (
	"context"
	"testing"
	"time"

	"example.com/phan-quyen-golang/internal/entitlement/application"
	entitlement "example.com/phan-quyen-golang/internal/entitlement/domain"
	entitlementinfra "example.com/phan-quyen-golang/internal/entitlement/infra"
	"example.com/phan-quyen-golang/internal/security/domain"
	securityinfra "example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestPlanMaterializesFeatureAndQuotaWithValidity(t *testing.T) {
	database := testutil.Database(t)
	if _, err := database.Exec(`INSERT INTO plans_v2(id,owner_type,owner_id,name,valid_from) VALUES('pro','system','system','pro','2020-01-01T00:00:00Z'); INSERT INTO plan_feature_rules_v2 VALUES('pro','invoice_management','allow'); INSERT INTO plan_quota_rules_v2 VALUES('pro','invoice_approvals','allow',3)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	subscription := entitlement.Subscription{ID: "subscription", PlanID: "pro", Subject: entitlement.Subject{Type: entitlement.SubjectOrganization, ID: "org-a"}, Effect: entitlement.EffectAllow, Status: entitlement.SubscriptionActive, ValidFrom: now.Add(-time.Hour), PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour)}
	manager := application.NewService(entitlementinfra.NewRepository(database))
	if err := manager.Activate(context.Background(), subscription); err != nil {
		t.Fatal(err)
	}
	security := securityinfra.NewRepository(database)
	subject := domain.Subject{Type: domain.SubjectType(subscription.Subject.Type), ID: subscription.Subject.ID}
	feature, err := security.HasFeature(context.Background(), subject, "invoice_management")
	if err != nil || !feature {
		t.Fatalf("feature=%v err=%v", feature, err)
	}
	quota, err := security.QuotaAvailable(context.Background(), subject, "invoice_approvals", 3)
	if err != nil || !quota {
		t.Fatalf("quota=%v err=%v", quota, err)
	}
	if err := manager.Cancel(context.Background(), subscription.ID, now); err != nil {
		t.Fatal(err)
	}
	plan, err := security.HasPlan(context.Background(), subject, "pro")
	if err != nil || plan {
		t.Fatalf("cancelled plan=%v err=%v", plan, err)
	}
}

func TestManualFeatureDenyOverridesPlanAllow(t *testing.T) {
	database := testutil.Database(t)
	if _, err := database.Exec(`INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('manual-deny','organization','org-a','invoice_management','deny','manual','override','1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	allowed, err := securityinfra.NewRepository(database).HasFeature(context.Background(), domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, "invoice_management")
	if err != nil || allowed {
		t.Fatalf("feature allowed=%v err=%v", allowed, err)
	}
}
