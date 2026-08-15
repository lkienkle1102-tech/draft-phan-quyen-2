package infra_test

import (
	"context"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestPermissionDenyWinsOnlyInsideSelectedScope(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	actor := domain.Actor{ID: "user-a", Type: domain.ActorUser}
	operation := domain.Operation{ResourceType: "invoice", Action: "approve"}
	orgA := domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}
	orgB := domain.Subject{Type: domain.SubjectOrganization, ID: "org-b"}
	if _, err := database.Exec(`INSERT INTO organization_members(organization_id,user_id,active) VALUES('org-b','user-a',1)`); err != nil {
		t.Fatal(err)
	}
	assertPermission(t, repository, actor, orgA, operation, true)
	if _, err := database.Exec(`INSERT INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from) VALUES('organization','org-b','user-a','invoice.approve','deny','1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertPermission(t, repository, actor, orgA, operation, true)
	assertPermission(t, repository, actor, orgB, operation, false)
	if _, err := database.Exec(`INSERT INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from) VALUES('organization','org-a','user-a','invoice.approve','deny','1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertPermission(t, repository, actor, orgA, operation, false)
}

func TestRolePermissionDenyWinsOverAnotherRoleAllow(t *testing.T) {
	assertSeededDeny(t, `INSERT INTO roles_v2(id,owner_type,owner_id,name) VALUES('org-a:denier','organization','org-a','denier'); INSERT INTO role_permission_rules_v2 VALUES('org-a:denier','invoice.approve','deny'); INSERT INTO role_assignments_v2(subject_type,subject_id,user_id,role_id,effect,valid_from) VALUES('organization','org-a','user-a','org-a:denier','allow','1970-01-01T00:00:00Z')`)
}

func TestFeatureDenyWinsAndIsSubjectScoped(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	if _, err := database.Exec(`INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('deny-feature','organization','org-a','invoice_management','deny','manual','test','1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertFeature(t, repository, domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, false)
	assertFeature(t, repository, domain.Subject{Type: domain.SubjectOrganization, ID: "org-b"}, true)
}

func TestGroupDenyWinsOverRoleAllow(t *testing.T) {
	assertSeededDeny(t, `
		INSERT INTO groups_v2(id,owner_type,owner_id,name) VALUES('org-a:blocked','organization','org-a','blocked');
		INSERT INTO group_memberships_v2(group_id,user_id,effect,valid_from) VALUES('org-a:blocked','user-a','deny','1970-01-01T00:00:00Z');
		INSERT INTO group_role_rules_v2(group_id,role_id,effect) VALUES('org-a:blocked','organization:org-a:finance','allow')`)
}

func assertSeededDeny(t *testing.T, statement string) {
	t.Helper()
	database := testutil.Database(t)
	if _, err := database.Exec(statement); err != nil {
		t.Fatal(err)
	}
	repository := infra.NewRepository(database)
	assertPermission(t, repository, domain.Actor{ID: "user-a", Type: domain.ActorUser}, domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, domain.Operation{ResourceType: "invoice", Action: "approve"}, false)
}

func assertPermission(t *testing.T, repository *infra.Repository, actor domain.Actor, subject domain.Subject, operation domain.Operation, wanted bool) {
	t.Helper()
	allowed, err := repository.HasPermission(context.Background(), actor, subject, operation)
	if err != nil || allowed != wanted {
		t.Fatalf("permission subject=%+v allowed=%v want=%v err=%v", subject, allowed, wanted, err)
	}
}

func assertFeature(t *testing.T, repository *infra.Repository, subject domain.Subject, wanted bool) {
	t.Helper()
	allowed, err := repository.HasFeature(context.Background(), subject, "invoice_management")
	if err != nil || allowed != wanted {
		t.Fatalf("feature subject=%+v allowed=%v want=%v err=%v", subject, allowed, wanted, err)
	}
}
