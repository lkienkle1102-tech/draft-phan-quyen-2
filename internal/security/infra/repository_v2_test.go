package infra_test

import (
	"context"
	"testing"

	"example.com/phan-quyen-golang/internal/security/domain"
	"example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/testutil"
)

func TestFeatureDenyWinsAndIsSubjectScoped(t *testing.T) {
	database := testutil.Database(t)
	repository := infra.NewRepository(database)
	if _, err := database.Exec(`INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('deny-feature','organization','org-a','invoice_management','deny','manual','test','1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	allowed, err := repository.HasFeature(context.Background(), domain.Subject{Type: domain.SubjectOrganization, ID: "org-a"}, "invoice_management")
	if err != nil || allowed {
		t.Fatalf("org-a allowed=%v err=%v", allowed, err)
	}
	allowed, err = repository.HasFeature(context.Background(), domain.Subject{Type: domain.SubjectOrganization, ID: "org-b"}, "invoice_management")
	if err != nil || !allowed {
		t.Fatalf("org-b allowed=%v err=%v", allowed, err)
	}
}

func TestProvisioningMembershipDoesNotPassHardMembershipGate(t *testing.T) {
	database := testutil.Database(t)
	if _, err := database.Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,provisioning,joined_at) VALUES('invitation:pending','org-b','user-personal',0,1,'2020-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	member, err := infra.NewRepository(database).IsMember(
		context.Background(),
		domain.Actor{ID: "user-personal", Type: domain.ActorUser},
		domain.Subject{ID: "org-b", Type: domain.SubjectOrganization},
	)
	if err != nil {
		t.Fatal(err)
	}
	if member {
		t.Fatal("provisioning membership passed the hard membership gate")
	}
}
