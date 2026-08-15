package app

import (
	"testing"
)

func TestAuthorizationV2SchemaRejectsInvalidData(t *testing.T) {
	application := newTestApp(t, "schema-v2.sqlite")
	database := application.Database()
	cases := []struct {
		name, statement string
	}{
		{"orphan role permission", `INSERT INTO role_permission_rules_v2 VALUES('missing','invoice.approve','allow')`},
		{"negative quota", `INSERT INTO subject_quota_entitlements_v2(id,subject_type,subject_id,quota_key,effect,quota_limit,used,reserved,period_start,source_type,source_id) VALUES('negative','user','user-personal','membership_applications','allow',5,-1,0,'2020-01-01T00:00:00Z','manual','test')`},
		{"invalid effect", `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('bad-effect','user','user-personal','organization_membership','maybe','manual','test','2020-01-01T00:00:00Z')`},
		{"cross-subject role", `INSERT INTO role_assignments_v2(subject_type,subject_id,user_id,role_id,effect,valid_from) VALUES('organization','org-b','user-b','organization:org-a:finance','allow','2020-01-01T00:00:00Z')`},
		{"orphan feature subject", `INSERT INTO subject_feature_entitlements_v2(id,subject_type,subject_id,feature_key,effect,source_type,source_id,valid_from) VALUES('orphan-subject','organization','missing','invoice_management','allow','manual','test','2020-01-01T00:00:00Z')`},
		{"non-member organization permission", `INSERT INTO subject_permission_rules_v2(subject_type,subject_id,user_id,permission_id,effect,valid_from) VALUES('organization','org-b','user-a','invoice.approve','allow','2020-01-01T00:00:00Z')`},
		{"active cross-organization grant", `INSERT INTO organization_access_grants(id,owner_organization_id,grantee_organization_id,resource_type,action,status) VALUES('forbidden','org-a','org-b','invoice','approve','active')`},
		{"ambiguous endpoint scope", `UPDATE endpoint_bindings SET scope_mode='either' WHERE id='invoice-approve'`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := database.Exec(item.statement); err == nil {
				t.Fatalf("invalid statement accepted: %s", item.statement)
			}
		})
	}
}

func TestPolicyNodeIdentityIsScopedByPolicyVersion(t *testing.T) {
	application := newTestApp(t, "policy-node-v2.sqlite")
	database := application.Database()
	if _, err := database.Exec(`INSERT INTO authorization_policies VALUES('policy-second',1,1); INSERT INTO policy_nodes(id,policy_id,policy_version,parent_id,node_type,rule_type,config_json,position) VALUES('root','policy-second',1,NULL,'ALL',NULL,'{}',0)`); err != nil {
		t.Fatal(err)
	}
}

func TestEveryRegisteredPrivateRouteHasActiveBinding(t *testing.T) {
	application := newTestApp(t, "route-registry.sqlite")
	for _, route := range []struct{ method, path string }{
		{"GET", "/v1/me"},
		{"POST", "/v1/organizations/:organizationID/invoices/:invoiceID/approve"},
		{"POST", "/v1/organizations/:organizationID/membership-applications"},
		{"POST", "/v1/organizations/:organizationID/membership-applications/:applicationID/review"},
		{"POST", "/v1/organizations/:organizationID/membership-invitations"},
		{"POST", "/v1/membership-invitations/accept"},
	} {
		var found bool
		err := application.Database().QueryRow(`SELECT EXISTS(SELECT 1 FROM endpoint_bindings WHERE method=? AND route_template=? AND active=1 AND policy_id<>'' AND scope_mode IN('user','organization') AND allow_personal_fallback=0)`, route.method, route.path).Scan(&found)
		if err != nil || !found {
			t.Fatalf("route %s %s binding=%v err=%v", route.method, route.path, found, err)
		}
	}
}
