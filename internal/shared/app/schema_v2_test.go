package app

import (
	"context"
	"testing"
)

func TestBusinessSchemaRejectsInvalidData(t *testing.T) {
	application := newTestApp(t, "schema-v2.sqlite")
	database := application.Database()
	cases := []struct {
		name, statement string
	}{
		{"negative quota", `INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,used,reserved,period_start,source_type,source_id) VALUES('negative','user','user-personal','membership_applications','allow',5,-1,0,'2020-01-01T00:00:00Z','manual','test')`},
		{"invalid effect", `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('bad-effect','user','user-personal','organization_membership','maybe','manual','test','2020-01-01T00:00:00Z')`},
		{"orphan feature subject", `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('orphan-subject','organization','missing','invoice_management','allow','manual','test','2020-01-01T00:00:00Z')`},
		{"invalid grant target", `INSERT INTO external_grants_v2(id,owner_organization_id,target_type,resource_type,action,effect,status,valid_from,created_by,created_at) VALUES('bad','org-a','global_user','invoice','approve','allow','active','2020-01-01T00:00:00Z','user-a','2020-01-01T00:00:00Z')`},
		{"active provisioning membership", `INSERT INTO organization_members(id,organization_id,user_id,active,provisioning,joined_at) VALUES('invalid-provisioning','org-b','user-personal',1,1,'2020-01-01T00:00:00Z')`},
		{"departed provisioning membership", `INSERT INTO organization_members(id,organization_id,user_id,active,provisioning,joined_at,left_at) VALUES('departed-provisioning','org-b','user-personal',0,1,'2020-01-01T00:00:00Z','2020-01-02T00:00:00Z')`},
		{"invalid invitation status", `INSERT INTO organization_invitations_v2(id,organization_id,user_id,token_hash,status,invited_by,valid_from,valid_until) VALUES('accepting','org-a','user-personal','accepting-hash','accepting','user-a','2020-01-01T00:00:00Z','2030-01-01T00:00:00Z')`},
		{"invalid acceptance attempt", `INSERT INTO invitation_acceptances_v2(invitation_id,membership_id,claim_id,lease_until,started_at,attempt_count) VALUES('missing','missing','claim','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',0)`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := database.Exec(item.statement); err == nil {
				t.Fatalf("invalid statement accepted: %s", item.statement)
			}
		})
	}
}

func TestProvisioningMembershipBlocksAnotherCurrentMembership(t *testing.T) {
	application := newTestApp(t, "provisioning-uniqueness.sqlite")
	database := application.Database()
	if _, err := database.Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,provisioning,joined_at) VALUES('provisioning','org-b','user-personal',0,1,'2020-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES('active','org-b','user-personal',1,'2020-01-01T00:00:00Z')`); err == nil {
		t.Fatal("active membership was created alongside provisioning membership")
	}
	var found bool
	if err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='invitation_acceptances_v2')`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("invitation acceptance coordination table is missing")
	}
}

func TestManualIAMAndPolicyTablesAreAbsent(t *testing.T) {
	application := newTestApp(t, "clean-schema.sqlite")
	removed := []string{
		"oauth_clients", "roles_v2", "role_assignments_v2", "groups_v2",
		"subject_permission_rules_v2", "authorization_policies", "policy_nodes",
		"policy_behaviors_v2", "policy_denials_v2", "endpoint_bindings",
		"organization_access_grants",
	}
	for _, name := range removed {
		var found bool
		if err := application.Database().QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, name).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found {
			t.Fatalf("manual IAM table %s still exists", name)
		}
	}
}

func TestEveryRegisteredPrivateRouteHasStaticBinding(t *testing.T) {
	catalog, err := securityCatalog()
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct{ method, path string }{
		{"GET", "/v1/me"},
		{"POST", "/v1/organizations/:organizationID/invoices/:invoiceID/approve"},
		{"POST", "/v1/organizations/:organizationID/membership-applications"},
		{"POST", "/v1/organizations/:organizationID/membership-applications/:applicationID/review"},
		{"POST", "/v1/organizations/:organizationID/membership-invitations"},
		{"POST", "/v1/membership-invitations/accept"},
		{"POST", "/v1/organizations/:organizationID/external-user-grants"},
		{"GET", "/v1/organizations/:organizationID/external-user-grants"},
		{"DELETE", "/v1/organizations/:organizationID/external-user-grants/:grantID"},
	}
	for _, route := range routes {
		binding, findErr := catalog.FindEndpoint(context.Background(), route.method, route.path)
		if findErr != nil || binding.PolicyID == "" {
			t.Fatalf("route %s %s binding=%+v err=%v", route.method, route.path, binding, findErr)
		}
	}
}
