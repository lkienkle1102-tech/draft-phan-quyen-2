package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	security "example.com/phan-quyen-golang/internal/security/domain"
)

func TestLowValueInvoiceUsesManualReviewStrategy(t *testing.T) {
	application := newTestApp(t, "behavior-flow.sqlite")
	actor := token(t, "user-a", "org-a", time.Now().UTC())
	response := approveRequest(application, "org-a", "invoice-low", actor, "manual-review")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Status string `json:"Status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "manual_review" {
		t.Fatalf("status=%q want manual_review", result.Status)
	}
	var used int64
	if err := application.Database().QueryRow(`SELECT SUM(used) FROM subject_quota_entitlements_v2 WHERE subject_type='organization' AND subject_id='org-a' AND quota_key='invoice_approvals'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 0 {
		t.Fatalf("manual review consumed approval quota: %d", used)
	}
}

func TestInvoiceReplayDoesNotConsumeQuotaTwice(t *testing.T) {
	application := newTestApp(t, "idempotency-flow.sqlite")
	actor := token(t, "user-a", "org-a", time.Now().UTC())
	first := approveRequest(application, "org-a", "invoice-a", actor, "replay")
	second := approveRequest(application, "org-a", "invoice-a", actor, "replay")
	if first.Code != http.StatusOK || second.Code != http.StatusOK || first.Body.String() != second.Body.String() {
		t.Fatalf("first=%d %s second=%d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	var used int64
	if err := application.Database().QueryRow(`SELECT SUM(used) FROM subject_quota_entitlements_v2 WHERE subject_type='organization' AND subject_id='org-a' AND quota_key='invoice_approvals'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 1 {
		t.Fatalf("idempotent replay quota used=%d want=1", used)
	}
}

func TestDeniedDecisionIsAudited(t *testing.T) {
	application := newTestApp(t, "deny-audit.sqlite")
	personal := token(t, "user-personal", "", time.Now().UTC())
	response := approveRequest(application, "org-a", "invoice-a", personal, "deny-audit")
	if response.Code == http.StatusOK {
		t.Fatal("personal user unexpectedly authorized")
	}
	var audited bool
	if err := application.Database().QueryRow(`SELECT EXISTS(SELECT 1 FROM audit_events_v2 WHERE actor_id='user-personal' AND outcome='deny')`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if !audited {
		t.Fatal("denied decision was not audited")
	}
}

func TestInvitationHTTPFlowAssignsOrganizationRole(t *testing.T) {
	application := newTestApp(t, "invitation-flow.sqlite")
	now := time.Now().UTC()
	inviter := token(t, "user-a", "org-a", now)
	body := bytes.NewBufferString(`{"user_id":"user-personal","role_ids":["organization:org-a:finance"],"valid_until":"` + now.Add(time.Hour).Format(time.RFC3339) + `"}`)
	invite := membershipRequest(application, http.MethodPost, "/v1/organizations/org-a/membership-invitations", inviter, body)
	if invite.Code != http.StatusCreated {
		t.Fatalf("invite status=%d body=%s", invite.Code, invite.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(invite.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	personal := token(t, "user-personal", "", now)
	acceptBody := bytes.NewBufferString(`{"token":"` + created.Token + `"}`)
	accepted := membershipRequest(application, http.MethodPost, "/v1/membership-invitations/accept", personal, acceptBody)
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	snapshot, err := application.directory.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	membershipID := "invitation:"
	var invitationID string
	if err = application.Database().QueryRow(`SELECT id FROM organization_invitations_v2 WHERE user_id='user-personal' AND status='accepted'`).Scan(&invitationID); err != nil {
		t.Fatal(err)
	}
	membershipID += invitationID
	domainID := "organization::org-a"
	userEdge := security.PolicyRule{PType: "g", V0: "user::user-personal", V1: "membership::" + membershipID, V2: domainID}
	roleEdge := security.PolicyRule{PType: "g", V0: "membership::" + membershipID, V1: "role::organization:org-a:finance", V2: domainID}
	if !slices.Contains(snapshot.Rules, userEdge) || !slices.Contains(snapshot.Rules, roleEdge) {
		t.Fatalf("accepted invitation policies=%+v", snapshot.Rules)
	}
	var active, provisioning, acceptances int
	if err = application.Database().QueryRow(`SELECT active,provisioning FROM organization_members WHERE id=?`, membershipID).Scan(&active, &provisioning); err != nil {
		t.Fatal(err)
	}
	if err = application.Database().QueryRow(`SELECT COUNT(*) FROM invitation_acceptances_v2 WHERE invitation_id=?`, invitationID).Scan(&acceptances); err != nil {
		t.Fatal(err)
	}
	if active != 1 || provisioning != 0 || acceptances != 0 {
		t.Fatalf("active=%d provisioning=%d acceptances=%d", active, provisioning, acceptances)
	}
}
