package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	security "example.com/phan-quyen-golang/internal/security/domain"
	securityinfra "example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/config"
)

const flowSecret = "01234567890123456789012345678901"

func TestAuthorizationFlowRejectsCrossOrganizationDelegation(t *testing.T) {
	application, err := New(config.Config{DatabasePath: t.TempDir() + "/flow.sqlite", JWTIssuer: "issuer", JWTAudience: "api", JWTSecret: flowSecret})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC()
	userA := token(t, "user-a", "org-a", now)
	response := approveRequest(application, "org-a", "invoice-a", userA, "own")
	if response.Code != http.StatusOK {
		t.Fatalf("own status=%d body=%s", response.Code, response.Body.String())
	}
	stale := token(t, "user-a", "org-a", now.Add(-time.Hour))
	response = approveRequest(application, "org-a", "invoice-a", stale, "stale")
	if response.Code != http.StatusForbidden {
		t.Fatalf("stale status=%d", response.Code)
	}
	userB := token(t, "user-b", "org-b", now)
	response = approveRequest(application, "org-a", "invoice-partner", userB, "missing")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing grant status=%d", response.Code)
	}
	validFrom := now.Add(-time.Minute).Format(time.RFC3339)
	_, err = application.Database().Exec(`INSERT INTO organization_access_grants(id,owner_organization_id,grantee_organization_id,resource_type,resource_id,action,valid_from,status,created_by) VALUES('grant','org-a','org-b','invoice','invoice-partner','approve',?,'active','user-a')`, validFrom)
	if err == nil {
		t.Fatal("active cross-organization grant was accepted")
	}
	response = approveRequest(application, "org-a", "invoice-partner", userB, "delegated")
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOrganizationExternalGrantFollowsCurrentMembership(t *testing.T) {
	application := newTestApp(t, "external-organization.sqlite")
	now := time.Now().UTC()
	repository := securityinfra.NewRepository(application.Database())
	limit := int64(3)
	grant := externalInvoiceGrant("org-b-bundle", security.ExternalGrantTarget{Type: security.ExternalTargetOrganization, OrganizationID: "org-b"}, limit, now)
	if err := repository.CreateExternalGrant(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	userB := token(t, "user-b", "org-b", now)
	response := approveRequest(application, "org-a", "invoice-partner", userB, "external-before-kick")
	if response.Code != http.StatusOK {
		t.Fatalf("before kick status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := application.Database().Exec(`UPDATE organization_members SET active=0,left_at=? WHERE organization_id='org-b' AND user_id='user-b' AND active=1; UPDATE invoices SET status='pending',version=1,approver_id=NULL WHERE id='invoice-partner'`, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	response = approveRequest(application, "org-a", "invoice-partner", userB, "external-after-kick")
	if response.Code != http.StatusNotFound {
		t.Fatalf("after kick status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := application.Database().Exec(`INSERT INTO organization_members(id,organization_id,user_id,active,joined_at) VALUES('org-b:user-b:rejoined','org-b','user-b',1,?)`, now.Add(time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	response = approveRequest(application, "org-a", "invoice-partner", userB, "external-after-rejoin")
	if response.Code != http.StatusOK {
		t.Fatalf("after rejoin status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGlobalUserExternalGrantDoesNotRequireOrganizationMembership(t *testing.T) {
	application := newTestApp(t, "external-global.sqlite")
	now := time.Now().UTC()
	repository := securityinfra.NewRepository(application.Database())
	limit := int64(1)
	if _, err := application.Database().Exec(`UPDATE invoices SET approver_id='user-personal' WHERE id='invoice-partner'`); err != nil {
		t.Fatal(err)
	}
	grant := externalInvoiceGrant("personal-bundle", security.ExternalGrantTarget{Type: security.ExternalTargetGlobalUser, UserID: "user-personal"}, limit, now)
	if err := repository.CreateExternalGrant(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	response := approveRequest(application, "org-a", "invoice-partner", token(t, "user-personal", "", now), "personal-external")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func externalInvoiceGrant(id string, target security.ExternalGrantTarget, limit int64, now time.Time) security.ExternalGrantDefinition {
	return security.ExternalGrantDefinition{ID: id, OwnerOrganizationID: "org-a", CreatedBy: "user-a", Target: target, Resource: security.Resource{Type: "invoice", ID: "invoice-partner", TenantID: "org-a"}, Operation: security.Operation{ResourceType: "invoice", Action: "approve"}, Effect: security.EffectAllow, ValidFrom: now.Add(-time.Minute), Permissions: []security.ExternalGrantItem{{Key: "invoice.approve", Effect: security.EffectAllow}}, Features: []security.ExternalGrantItem{{Key: "invoice_management", Effect: security.EffectAllow}}, Quotas: []security.ExternalGrantItem{{Key: "invoice_approvals", Effect: security.EffectAllow, Limit: &limit}}}
}

func TestExternalGrantManagementAPIIsInternalAndImmutable(t *testing.T) {
	application := newTestApp(t, "external-management.sqlite")
	manager := token(t, "user-a", "org-a", time.Now().UTC())
	body := bytes.NewBufferString(`{"target":{"type":"organization","organization_id":"org-b"},"resource_type":"invoice","resource_id":"invoice-partner","action":"approve","effect":"allow","permissions":[{"key":"invoice.approve","effect":"allow"}],"features":[{"key":"invoice_management","effect":"allow"}],"quotas":[{"key":"invoice_approvals","effect":"allow","limit":2}]}`)
	response := membershipRequest(application, http.MethodPost, "/v1/organizations/org-a/external-user-grants", manager, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("missing grant id")
	}
	response = membershipRequest(application, http.MethodGet, "/v1/organizations/org-a/external-user-grants", manager, nil)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(created.ID)) {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	response = membershipRequest(application, http.MethodDelete, "/v1/organizations/org-a/external-user-grants/"+created.ID, manager, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", response.Code, response.Body.String())
	}
	var status string
	if err := application.Database().QueryRow(`SELECT status FROM external_grants_v2 WHERE id=?`, created.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "revoked" {
		t.Fatalf("status=%s", status)
	}
	// The management capability itself is non-delegable.
	definition := security.ExternalGrantDefinition{ID: "escalation", OwnerOrganizationID: "org-a", CreatedBy: "user-a", Target: security.ExternalGrantTarget{Type: security.ExternalTargetGlobalUser, UserID: "user-personal"}, Resource: security.Resource{Type: "external_grant", TenantID: "org-a"}, Operation: security.Operation{ResourceType: "external_grant", Action: "manage"}, Effect: security.EffectAllow, ValidFrom: time.Now().UTC(), Permissions: []security.ExternalGrantItem{{Key: "external_grant.manage", Effect: security.EffectAllow}}}
	if err := securityinfra.NewRepository(application.Database()).CreateExternalGrant(context.Background(), definition); err == nil {
		t.Fatal("external_grant.manage was delegated")
	}
}

func TestPersonalUserAppliesAndOrganizationApprovesMembership(t *testing.T) {
	application := newTestApp(t, "membership.sqlite")
	now := time.Now().UTC()
	personal := token(t, "user-personal", "", now)

	invoiceResponse := approveRequest(application, "org-a", "invoice-a", personal, "personal-invoice")
	if invoiceResponse.Code == http.StatusOK {
		t.Fatal("personal user bypassed organization-scoped invoice authorization")
	}

	applyResponse := membershipRequest(application, http.MethodPost, "/v1/organizations/org-a/membership-applications", personal, nil)
	if applyResponse.Code != http.StatusAccepted {
		t.Fatalf("apply status=%d body=%s", applyResponse.Code, applyResponse.Body.String())
	}
	var applied struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(applyResponse.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}

	if activeMember(t, application, "org-a", "user-personal") {
		t.Fatal("pending application activated membership")
	}

	claimedOrg := token(t, "user-personal", "org-a", now)
	beforeApproval := approveRequest(application, "org-a", "invoice-a", claimedOrg, "before-membership")
	if beforeApproval.Code != http.StatusNotFound {
		t.Fatalf("inactive member status=%d body=%s", beforeApproval.Code, beforeApproval.Body.String())
	}

	reviewer := token(t, "user-a", "org-a", now)
	reviewResponse := membershipRequest(application, http.MethodPost, "/v1/organizations/org-a/membership-applications/"+applied.ID+"/review", reviewer, bytes.NewBufferString(`{"approve":true}`))
	if reviewResponse.Code != http.StatusNoContent {
		t.Fatalf("review status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	if !activeMember(t, application, "org-a", "user-personal") {
		t.Fatal("approved application did not activate membership")
	}
	var used int64
	if err := application.Database().QueryRow(`SELECT SUM(used) FROM subject_quota_entitlements_v2 WHERE subject_type='user' AND subject_id='user-personal' AND quota_key='membership_applications'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 1 {
		t.Fatalf("personal quota used=%d want=1", used)
	}
}

func newTestApp(t *testing.T, name string) *App {
	t.Helper()
	application, err := New(config.Config{DatabasePath: t.TempDir() + "/" + name, JWTIssuer: "issuer", JWTAudience: "api", JWTSecret: flowSecret})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil {
			t.Error(err)
		}
	})
	return application
}

func membershipRequest(application *App, method, path, tokenValue string, body *bytes.Buffer) *httptest.ResponseRecorder {
	var requestBody io.Reader
	if body != nil {
		requestBody = body
	}
	request := httptest.NewRequest(method, path, requestBody)
	request.Header.Set("Authorization", "Bearer "+tokenValue)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.ServeHTTP(response, request)
	return response
}

func activeMember(t *testing.T, application *App, organizationID, userID string) bool {
	t.Helper()
	var active bool
	if err := application.Database().QueryRow(`SELECT EXISTS(SELECT 1 FROM organization_members WHERE organization_id=? AND user_id=? AND active=1)`, organizationID, userID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	return active
}

func TestMemberSpecificOrganizationGrantCannotBeActivated(t *testing.T) {
	application, err := New(config.Config{DatabasePath: t.TempDir() + "/grant.sqlite", JWTIssuer: "issuer", JWTAudience: "api", JWTSecret: flowSecret})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil {
			t.Error(err)
		}
	})
	db := application.Database()
	if _, err := db.Exec(`INSERT INTO users VALUES('user-c','org-b',1); INSERT INTO organization_members VALUES('org-b','user-c',1); INSERT INTO organization_access_grants(id,owner_organization_id,grantee_organization_id,resource_type,resource_id,action,valid_from,status,created_by,grantee_user_id) VALUES('member-grant','org-a','org-b','invoice','invoice-partner','approve',?,'active','user-a','user-b')`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err == nil {
		t.Fatal("member-specific cross-organization grant was accepted")
	}
	repository := securityinfra.NewRepository(db)
	request := security.GrantRequest{OwnerOrganizationID: "org-a", GranteeOrganizationID: "org-b", Resource: security.Resource{Type: "invoice", ID: "invoice-partner"}, Operation: security.Operation{ResourceType: "invoice", Action: "approve"}, At: time.Now().UTC()}
	request.ActorID = "user-b"
	_, allowed, err := repository.FindGrant(context.Background(), request)
	if err != nil || allowed {
		t.Fatalf("specific member grant allowed=%v err=%v", allowed, err)
	}
	request.ActorID = "user-c"
	_, allowed, err = repository.FindGrant(context.Background(), request)
	if err != nil || allowed {
		t.Fatalf("other member allowed=%v err=%v", allowed, err)
	}
}

func TestRejectedMembershipDoesNotActivateAndCannotBeReviewedTwice(t *testing.T) {
	application := newTestApp(t, "membership-reject.sqlite")
	now := time.Now().UTC()
	personal := token(t, "user-personal", "", now)
	applicationID := applyMembership(t, application, "org-b", personal)
	reviewer := token(t, "user-b", "org-b", now)
	path := "/v1/organizations/org-b/membership-applications/" + applicationID + "/review"
	response := membershipRequest(application, http.MethodPost, path, reviewer, bytes.NewBufferString(`{"approve":false}`))
	if response.Code != http.StatusNoContent {
		t.Fatalf("reject status=%d body=%s", response.Code, response.Body.String())
	}
	if activeMember(t, application, "org-b", "user-personal") {
		t.Fatal("rejected application activated membership")
	}
	response = membershipRequest(application, http.MethodPost, path, reviewer, bytes.NewBufferString(`{"approve":true}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("second review status=%d body=%s", response.Code, response.Body.String())
	}
}

func applyMembership(t *testing.T, application *App, organizationID, tokenValue string) string {
	t.Helper()
	response := membershipRequest(application, http.MethodPost, "/v1/organizations/"+organizationID+"/membership-applications", tokenValue, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("apply status=%d body=%s", response.Code, response.Body.String())
	}
	var applied struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	return applied.ID
}

func approveRequest(application *App, organization, invoice, tokenValue, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/organizations/"+organization+"/invoices/"+invoice+"/approve", bytes.NewBufferString(`{"version":1}`))
	request.Header.Set("Authorization", "Bearer "+tokenValue)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	application.ServeHTTP(response, request)
	return response
}

func token(t *testing.T, user, organization string, authTime time.Time) string {
	return actorToken(t, user, organization, authTime, "user", "")
}

func actorToken(t *testing.T, subject, organization string, authTime time.Time, actorType, clientID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"sub":%q,"iss":"issuer","aud":"api","exp":%d,"nbf":%d,"actor_type":%q,"client_id":%q,"organization_id":%q,"amr":["mfa"],"auth_time":%d}`, subject, time.Now().Add(time.Hour).Unix(), time.Now().Add(-time.Minute).Unix(), actorType, clientID, organization, authTime.Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	mac := hmac.New(sha256.New, []byte(flowSecret))
	_, _ = mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
