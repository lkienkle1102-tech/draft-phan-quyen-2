package infra_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	identity "example.com/phan-quyen-golang/internal/identity/domain"
	identityinfra "example.com/phan-quyen-golang/internal/identity/infra"
	security "example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestReadSeparatesScopesAndAppliesDenyWins(t *testing.T) {
	database := testutil.Database(t)
	now := time.Now().UTC()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES('org-b:user-a','org-b','user-a',1,?)`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO role_assignments_v2(subject_type,subject_id,user_id,role_id,effect,valid_from) VALUES('organization','org-b','user-a','organization:org-b:finance','allow',?)`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from) VALUES('organization','org-b','user-a','invoice.approve','deny',?)`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from) VALUES('user','user-a','user-a','invoice.approve','allow',?)`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('org-b-feature-deny','organization','org-b','invoice_management','deny','manual','test',?)`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,period_start,source_type,source_id) VALUES('user-a-unlimited','user','user-a','invoice_approvals','allow',NULL,?,'manual','test')`, []any{now.Add(-time.Hour).Format(time.RFC3339)}},
		{`INSERT INTO features_v2(key) VALUES('expired_feature'); INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from,valid_until) VALUES('expired-user-feature','user','user-a','expired_feature','allow','manual','test',?,?)`, []any{now.Add(-2 * time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339)}},
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	insertPlan(t, database, now)
	snapshot, err := identityinfra.NewRepository(database).Read(context.Background(), security.Actor{ID: "user-a", Type: security.ActorUser}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertSeparatedSnapshot(t, snapshot)
}

func assertSeparatedSnapshot(t *testing.T, snapshot identity.Snapshot) {
	t.Helper()
	if effect(snapshot.Personal.Entitlements.Permissions, "invoice.approve") != "allow" {
		t.Fatal("personal permission was not independently allowed")
	}
	orgA := organization(t, snapshot, "org-a")
	orgB := organization(t, snapshot, "org-b")
	if effect(orgA.Entitlements.Permissions, "invoice.approve") != "allow" {
		t.Fatal("org-a permission was not allowed")
	}
	if effect(orgB.Entitlements.Permissions, "invoice.approve") != "deny" {
		t.Fatal("org-b direct deny did not win over its role allow")
	}
	if effect(orgB.Entitlements.Features, "invoice_management") != "deny" {
		t.Fatal("org-b feature deny did not win")
	}
	if snapshot.Personal.Entitlements.Plan == nil || snapshot.Personal.Entitlements.Plan.ID != "personal-pro" {
		t.Fatal("active personal plan missing")
	}
	if effect(snapshot.Personal.Entitlements.Features, "expired_feature") != "" {
		t.Fatal("expired feature remained effective")
	}
	personalQuota := quota(t, snapshot.Personal.Entitlements.Quotas, "invoice_approvals")
	if !personalQuota.Unlimited || personalQuota.Limit != nil || personalQuota.Remaining != nil {
		t.Fatalf("unlimited quota=%+v", personalQuota)
	}
	organizationQuota := quota(t, orgA.Entitlements.Quotas, "invoice_approvals")
	if organizationQuota.Limit == nil || *organizationQuota.Limit != 100 || organizationQuota.Remaining == nil || *organizationQuota.Remaining != 100 {
		t.Fatalf("organization quota=%+v", organizationQuota)
	}
}

func TestReadExternalGrantTargetsFollowMembershipLifecycle(t *testing.T) {
	database := testutil.Database(t)
	now := time.Now().UTC()
	var membershipID string
	if err := database.QueryRow(`SELECT id FROM organization_members WHERE organization_id='org-b' AND user_id='user-b' AND active=1`).Scan(&membershipID); err != nil {
		t.Fatal(err)
	}
	insertGrant(t, database, "global", "global_user", "user-b", "", "", now)
	insertGrant(t, database, "organization", "organization", "", "org-b", "", now)
	insertGrant(t, database, "member", "organization_member", "user-b", "org-b", membershipID, now)
	repository := identityinfra.NewRepository(database)
	assertGrantIDs(t, read(t, repository, now), "global", "member", "organization")
	if _, err := database.Exec(`UPDATE organization_members SET active=0,left_at=? WHERE id=?`, now.Format(time.RFC3339), membershipID); err != nil {
		t.Fatal(err)
	}
	assertGrantIDs(t, read(t, repository, now.Add(time.Minute)), "global")
	if _, err := database.Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES('org-b:user-b:new','org-b','user-b',1,?)`, now.Add(time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	assertGrantIDs(t, read(t, repository, now.Add(2*time.Minute)), "global", "organization")
}

func insertPlan(t *testing.T, database *sql.DB, now time.Time) {
	t.Helper()
	from, until := now.Add(-time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO plans_v2(id,owner_type,owner_id,name,active,valid_from,valid_until) VALUES('personal-pro','system','system','Personal Pro',1,?,?);
		INSERT INTO subscriptions_v2(id,subject_type,subject_id,plan_id,effect,status,valid_from,valid_until,current_period_start,current_period_end) VALUES('user-a-plan','user','user-a','personal-pro','allow','active',?,?,?,?)`, from, until, from, until, from, until); err != nil {
		t.Fatal(err)
	}
}

func insertGrant(t *testing.T, database *sql.DB, id, targetType, userID, organizationID, membershipID string, now time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO external_grants_v2(id,owner_organization_id,target_type,target_user_id,target_organization_id,target_membership_id,resource_type,resource_id,action,effect,status,valid_from,created_by,created_at)
		VALUES(?,'org-a',?,?,?,?, 'invoice','invoice-partner','approve','allow','active',?,'user-a',?)`, id, targetType, nullable(userID), nullable(organizationID), nullable(membershipID), now.Add(-time.Hour).Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func read(t *testing.T, repository *identityinfra.Repository, now time.Time) identity.Snapshot {
	t.Helper()
	snapshot, err := repository.Read(context.Background(), security.Actor{ID: "user-b", Type: security.ActorUser}, now)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func organization(t *testing.T, snapshot identity.Snapshot, id string) identity.OrganizationScope {
	t.Helper()
	for _, value := range snapshot.Organizations {
		if value.Organization.ID == id {
			return value
		}
	}
	t.Fatalf("organization %s missing", id)
	return identity.OrganizationScope{}
}

func effect(values []identity.EffectiveFact, key string) string {
	for _, value := range values {
		if value.Key == key {
			return value.Effect
		}
	}
	return ""
}

func quota(t *testing.T, values []identity.Quota, key string) identity.Quota {
	t.Helper()
	for _, value := range values {
		if value.Key == key {
			return value
		}
	}
	t.Fatalf("quota %s missing", key)
	return identity.Quota{}
}

func assertGrantIDs(t *testing.T, snapshot identity.Snapshot, wanted ...string) {
	t.Helper()
	ids := make([]string, 0, len(snapshot.ExternalGrants))
	for _, value := range snapshot.ExternalGrants {
		ids = append(ids, value.ID)
	}
	if !slices.Equal(ids, wanted) {
		t.Fatalf("grant ids=%v want=%v", ids, wanted)
	}
}
