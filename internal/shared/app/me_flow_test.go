package app

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type meResponse struct {
	Identity struct {
		ID        string `json:"id"`
		ActorType string `json:"actor_type"`
	} `json:"identity"`
	Personal struct {
		Subject struct {
			Type, ID string
		} `json:"subject"`
		Permissions []struct{ Key, Effect string } `json:"permissions"`
	} `json:"personal"`
	Organizations []struct {
		Organization struct{ ID string } `json:"organization"`
		Membership   struct{ ID string } `json:"membership"`
	} `json:"organizations"`
	ExternalGrants []any `json:"external_grants"`
}

func TestMeReturnsScopeSeparatedSnapshot(t *testing.T) {
	application := newTestApp(t, "me.sqlite")
	response := membershipRequest(application, http.MethodGet, "/v1/me", token(t, "user-a", "org-a", time.Now().UTC()), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body meResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	assertMeResponse(t, body)
	assertMeAudit(t, application)
}

func assertMeResponse(t *testing.T, body meResponse) {
	t.Helper()
	if body.Identity.ID != "user-a" || body.Identity.ActorType != "user" {
		t.Fatalf("identity=%+v", body.Identity)
	}
	if body.Personal.Subject.Type != "user" || body.Personal.Subject.ID != "user-a" {
		t.Fatalf("personal subject=%+v", body.Personal.Subject)
	}
	if len(body.Personal.Permissions) != 1 || body.Personal.Permissions[0].Key != "identity.read_self" || body.Personal.Permissions[0].Effect != "allow" {
		t.Fatalf("personal permissions=%+v", body.Personal.Permissions)
	}
	if len(body.Organizations) != 1 || body.Organizations[0].Organization.ID != "org-a" || body.Organizations[0].Membership.ID == "" {
		t.Fatalf("organizations=%+v", body.Organizations)
	}
	if body.ExternalGrants == nil {
		t.Fatal("external_grants must be an array")
	}
}

func assertMeAudit(t *testing.T, application *App) {
	t.Helper()
	var audited bool
	if err := application.Database().QueryRow(`SELECT EXISTS(SELECT 1 FROM audit_events_v2 WHERE actor_id='user-a' AND action='read_self' AND outcome='allow')`).Scan(&audited); err != nil || !audited {
		t.Fatalf("audit=%v err=%v", audited, err)
	}
}

func TestMeRejectsInvalidAndMachineActors(t *testing.T) {
	application := newTestApp(t, "me-auth.sqlite")
	response := membershipRequest(application, http.MethodGet, "/v1/me", "invalid", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status=%d", response.Code)
	}
	machine := actorToken(t, "machine-subject", "org-a", time.Now().UTC(), "machine", "client-me")
	response = membershipRequest(application, http.MethodGet, "/v1/me", machine, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("machine status=%d body=%s", response.Code, response.Body.String())
	}
}
