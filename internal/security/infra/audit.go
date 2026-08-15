package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

func (r *Repository) AuditDecision(ctx context.Context, request domain.Request, decision domain.Decision, outcome, reason string) error {
	id, err := auditID()
	if err != nil {
		return err
	}
	_, err = r.database.ExecContext(ctx, `INSERT INTO audit_events_v2(id,occurred_at,actor_id,client_id,subject_type,subject_id,resource_type,resource_id,action,outcome,reason,policy_id,policy_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, time.Now().UTC().Format(time.RFC3339), request.Actor.ID, nullableString(request.Actor.ClientID), nullableString(string(request.Subject.Type)), nullableString(request.Subject.ID), nullableString(request.Primary.Type), nullableString(request.Primary.ID), request.Operation.Action, outcome, reason, nullableString(decision.PolicyID), nullableVersion(decision.PolicyVersion))
	return err
}

func auditID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableVersion(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
