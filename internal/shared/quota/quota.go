package quota

import (
	"context"
	"database/sql"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type Operation struct {
	ID, ActorID, ResourceType, Action string
	ExpiresAt                         time.Time
}

func ConsumeQuotas(ctx context.Context, tx *sql.Tx, costs []domain.QuotaCost, operation Operation) error {
	if err := ReserveQuotas(ctx, tx, costs, operation); err != nil {
		return err
	}
	return CommitQuotas(ctx, tx, operation)
}

func ReserveQuotas(ctx context.Context, tx *sql.Tx, costs []domain.QuotaCost, operation Operation) error {
	for _, cost := range costs {
		if cost.Cost <= 0 || cost.GrantID != "" {
			return domain.ErrQuotaExceeded
		}
		if len(cost.ExternalGrantIDs) > 0 {
			if err := reserveExternalCost(ctx, tx, cost, operation); err != nil {
				return err
			}
			continue
		}
		reservations, err := reserveCost(ctx, tx, cost)
		if err != nil {
			return err
		}
		for _, reservation := range reservations {
			if _, err := tx.ExecContext(ctx, `INSERT INTO quota_reservations_v2(id,subject_type,subject_id,quota_entitlement_id,amount,status,expires_at) VALUES(?,?,?,?,?,'reserved',?)`, operation.ID, cost.Subject.Type, cost.Subject.ID, reservation.entitlementID, reservation.amount, operation.ExpiresAt.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if err := writeLedger(ctx, tx, operation, cost, "reserve"); err != nil {
			return err
		}
	}
	return nil
}

type quotaReservation struct {
	entitlementID string
	amount        int64
}

func reserveCost(ctx context.Context, tx *sql.Tx, cost domain.QuotaCost) ([]quotaReservation, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := tx.QueryContext(ctx, `SELECT id,quota_limit,used,reserved FROM subject_quota_entitlements_v2
		WHERE subject_type=? AND subject_id=? AND quota_key=? AND effect='allow'
		  AND period_start<=? AND(period_end IS NULL OR period_end>?)
		  AND NOT EXISTS(SELECT 1 FROM subject_quota_entitlements_v2 denied WHERE denied.subject_type=? AND denied.subject_id=? AND denied.quota_key=? AND denied.effect='deny' AND denied.period_start<=? AND(denied.period_end IS NULL OR denied.period_end>?))
		ORDER BY CASE WHEN quota_limit IS NULL THEN 1 ELSE 0 END,period_end,id`, cost.Subject.Type, cost.Subject.ID, cost.QuotaKey, now, now, cost.Subject.Type, cost.Subject.ID, cost.QuotaKey, now, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	remaining := cost.Cost
	var reservations []quotaReservation
	for rows.Next() {
		var id string
		var limit sql.NullInt64
		var used, reserved int64
		if err := rows.Scan(&id, &limit, &used, &reserved); err != nil {
			return nil, err
		}
		amount := remaining
		if limit.Valid && limit.Int64-used-reserved < amount {
			amount = limit.Int64 - used - reserved
		}
		if amount > 0 {
			reservations = append(reservations, quotaReservation{entitlementID: id, amount: amount})
			remaining -= amount
		}
		if remaining == 0 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if remaining != 0 {
		return nil, domain.ErrQuotaExceeded
	}
	for _, reservation := range reservations {
		result, err := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved+? WHERE id=? AND effect='allow' AND(quota_limit IS NULL OR used+reserved+?<=quota_limit)`, reservation.amount, reservation.entitlementID, reservation.amount)
		if err != nil {
			return nil, err
		}
		if err := exactlyOneQuota(result); err != nil {
			return nil, err
		}
	}
	return reservations, nil
}

func CommitQuotas(ctx context.Context, tx *sql.Tx, operation Operation) error {
	records, err := loadReservations(ctx, tx, operation.ID)
	if err != nil {
		return err
	}
	committed := map[string]domain.QuotaCost{}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved-?,used=used+? WHERE id=? AND reserved>=?`, record.amount, record.amount, record.entitlementID, record.amount); err != nil {
			return err
		}
		key := record.subjectType + ":" + record.subjectID + ":" + record.quotaKey
		cost := committed[key]
		cost.Subject = domain.Subject{Type: domain.SubjectType(record.subjectType), ID: record.subjectID}
		cost.QuotaKey = record.quotaKey
		cost.Cost += record.amount
		committed[key] = cost
	}
	for _, cost := range committed {
		if err := writeLedger(ctx, tx, operation, cost, "commit"); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE quota_reservations_v2 SET status='committed' WHERE id=? AND status='reserved'`, operation.ID)
	if err != nil {
		return err
	}
	return commitExternal(ctx, tx, operation)
}

func ReleaseQuotas(ctx context.Context, tx *sql.Tx, operation Operation) error {
	if err := finishReservations(ctx, tx, operation, "released", "release"); err != nil {
		return err
	}
	return releaseExternal(ctx, tx, operation, "released")
}

func ExpireReservations(ctx context.Context, tx *sql.Tx, at time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT id FROM quota_reservations_v2 WHERE status='reserved' AND expires_at<=?`, at.UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := finishReservations(ctx, tx, Operation{ID: id, ActorID: "system", ResourceType: "quota", Action: "expire"}, "expired", "expire"); err != nil {
			return err
		}
	}
	return rows.Err()
}

func reserveExternalCost(ctx context.Context, tx *sql.Tx, cost domain.QuotaCost, operation Operation) error {
	remaining := cost.Cost
	for _, grantID := range cost.ExternalGrantIDs {
		rows, err := tx.QueryContext(ctx, `SELECT owner_entitlement_id,allocated-used-reserved FROM external_grant_quota_allocations_v2 WHERE grant_id=? AND quota_key=? AND effect='allow' ORDER BY owner_entitlement_id`, grantID, cost.QuotaKey)
		if err != nil {
			return err
		}
		for rows.Next() && remaining > 0 {
			var entitlementID string
			var available int64
			if err := rows.Scan(&entitlementID, &available); err != nil {
				_ = rows.Close()
				return err
			}
			amount := available
			if amount > remaining {
				amount = remaining
			}
			if amount <= 0 {
				continue
			}
			result, err := tx.ExecContext(ctx, `UPDATE external_grant_quota_allocations_v2 SET reserved=reserved+? WHERE grant_id=? AND quota_key=? AND owner_entitlement_id=? AND effect='allow' AND used+reserved+?<=allocated`, amount, grantID, cost.QuotaKey, entitlementID, amount)
			if err != nil {
				_ = rows.Close()
				return err
			}
			if err := exactlyOneQuota(result); err != nil {
				_ = rows.Close()
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO external_grant_quota_reservations_v2(operation_id,grant_id,quota_key,owner_entitlement_id,amount,status,expires_at) VALUES(?,?,?,?,?,'reserved',?)`, operation.ID, grantID, cost.QuotaKey, entitlementID, amount, operation.ExpiresAt.UTC().Format(time.RFC3339)); err != nil {
				_ = rows.Close()
				return err
			}
			remaining -= amount
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if remaining == 0 {
			break
		}
	}
	if remaining != 0 {
		return domain.ErrQuotaExceeded
	}
	return nil
}

func commitExternal(ctx context.Context, tx *sql.Tx, operation Operation) error {
	rows, err := tx.QueryContext(ctx, `SELECT grant_id,quota_key,owner_entitlement_id,amount FROM external_grant_quota_reservations_v2 WHERE operation_id=? AND status='reserved'`, operation.ID)
	if err != nil {
		return err
	}
	type item struct {
		grant, key, owner string
		amount            int64
	}
	var items []item
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.grant, &v.key, &v.owner, &v.amount); err != nil {
			_ = rows.Close()
			return err
		}
		items = append(items, v)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, v := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE external_grant_quota_allocations_v2 SET reserved=reserved-?,used=used+? WHERE grant_id=? AND quota_key=? AND owner_entitlement_id=? AND reserved>=?`, v.amount, v.amount, v.grant, v.key, v.owner, v.amount); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved-?,used=used+? WHERE id=? AND reserved>=?`, v.amount, v.amount, v.owner, v.amount)
		if err != nil {
			return err
		}
		if err := exactlyOneQuota(result); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE external_grant_quota_reservations_v2 SET status='committed' WHERE operation_id=? AND status='reserved'`, operation.ID)
	return err
}

func releaseExternal(ctx context.Context, tx *sql.Tx, operation Operation, status string) error {
	rows, err := tx.QueryContext(ctx, `SELECT grant_id,quota_key,owner_entitlement_id,amount FROM external_grant_quota_reservations_v2 WHERE operation_id=? AND status='reserved'`, operation.ID)
	if err != nil {
		return err
	}
	type item struct {
		grant, key, owner string
		amount            int64
	}
	var items []item
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.grant, &v.key, &v.owner, &v.amount); err != nil {
			_ = rows.Close()
			return err
		}
		items = append(items, v)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, v := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE external_grant_quota_allocations_v2 SET reserved=reserved-? WHERE grant_id=? AND quota_key=? AND owner_entitlement_id=? AND reserved>=?`, v.amount, v.grant, v.key, v.owner, v.amount); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE external_grant_quota_reservations_v2 SET status=? WHERE operation_id=? AND status='reserved'`, status, operation.ID)
	return err
}

func finishReservations(ctx context.Context, tx *sql.Tx, operation Operation, status, ledgerOperation string) error {
	records, err := loadReservations(ctx, tx, operation.ID)
	if err != nil {
		return err
	}
	finished := map[string]domain.QuotaCost{}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `UPDATE subject_quota_entitlements_v2 SET reserved=reserved-? WHERE id=? AND reserved>=?`, record.amount, record.entitlementID, record.amount); err != nil {
			return err
		}
		key := record.subjectType + ":" + record.subjectID + ":" + record.quotaKey
		cost := finished[key]
		cost.Subject = domain.Subject{Type: domain.SubjectType(record.subjectType), ID: record.subjectID}
		cost.QuotaKey = record.quotaKey
		cost.Cost += record.amount
		finished[key] = cost
	}
	for _, cost := range finished {
		if err := writeLedger(ctx, tx, operation, cost, ledgerOperation); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE quota_reservations_v2 SET status=? WHERE id=? AND status='reserved'`, status, operation.ID)
	return err
}

type reservationRecord struct {
	entitlementID, subjectType, subjectID, quotaKey string
	amount                                          int64
}

func loadReservations(ctx context.Context, tx *sql.Tx, id string) ([]reservationRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT reservation.quota_entitlement_id,reservation.amount,reservation.subject_type,reservation.subject_id,entitlement.quota_key FROM quota_reservations_v2 reservation JOIN subject_quota_entitlements_v2 entitlement ON entitlement.id=reservation.quota_entitlement_id WHERE reservation.id=? AND reservation.status='reserved'`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var records []reservationRecord
	for rows.Next() {
		var record reservationRecord
		if err := rows.Scan(&record.entitlementID, &record.amount, &record.subjectType, &record.subjectID, &record.quotaKey); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func writeLedger(ctx context.Context, tx *sql.Tx, operation Operation, cost domain.QuotaCost, kind string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO quota_ledger_v2(idempotency_key,actor_id,subject_type,subject_id,resource_type,action,quota_key,amount,operation,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, operation.ID, operation.ActorID, cost.Subject.Type, cost.Subject.ID, operation.ResourceType, operation.Action, cost.QuotaKey, cost.Cost, kind, time.Now().UTC().Format(time.RFC3339))
	return err
}

func exactlyOneQuota(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrQuotaExceeded
	}
	return nil
}
