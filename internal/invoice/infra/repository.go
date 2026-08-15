// Package infra implements invoice persistence.
package infra

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	invoiceapp "example.com/phan-quyen-golang/internal/invoice/application"
	invoice "example.com/phan-quyen-golang/internal/invoice/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
	sharedquota "example.com/phan-quyen-golang/internal/shared/quota"
)

type transaction struct{ value *sql.Tx }

func (transaction) TransactionMarker() {}

type Store struct{ database *sql.DB }

func NewStore(database *sql.DB) *Store { return &Store{database: database} }
func (s *Store) Within(ctx context.Context, operation func(invoiceapp.Transaction) error) (returnErr error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, tx.Rollback())
		}
	}()
	if err := operation(transaction{tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func sqlTx(value invoiceapp.Transaction) *sql.Tx { return value.(transaction).value }
func (s *Store) Find(ctx context.Context, tx invoiceapp.Transaction, id string) (invoice.Invoice, error) {
	var value invoice.Invoice
	err := sqlTx(tx).QueryRowContext(ctx, `SELECT id,organization_id,owner_id,COALESCE(approver_id,''),status,amount,region,version FROM invoices WHERE id=?`, id).Scan(&value.ID, &value.OrganizationID, &value.OwnerID, &value.ApproverID, &value.Status, &value.Amount, &value.Region, &value.Version)
	return value, err
}
func (s *Store) Approve(ctx context.Context, tx invoiceapp.Transaction, id string, version int64) (invoice.Invoice, error) {
	return s.transition(ctx, tx, id, version, "approved")
}

func (s *Store) RequestManualReview(ctx context.Context, tx invoiceapp.Transaction, id string, version int64) (invoice.Invoice, error) {
	return s.transition(ctx, tx, id, version, "manual_review")
}

func (s *Store) transition(ctx context.Context, tx invoiceapp.Transaction, id string, version int64, status string) (invoice.Invoice, error) {
	result, err := sqlTx(tx).ExecContext(ctx, `UPDATE invoices SET status=?,version=version+1 WHERE id=? AND status='pending' AND version=?`, status, id, version)
	if err != nil {
		return invoice.Invoice{}, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return invoice.Invoice{}, errors.New("invoice version conflict")
	}
	return s.Find(ctx, tx, id)
}
func (s *Store) Replay(ctx context.Context, tx invoiceapp.Transaction, key, hash string) (invoice.Invoice, bool, error) {
	var storedHash, raw string
	err := sqlTx(tx).QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_records WHERE key=?`, key).Scan(&storedHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return invoice.Invoice{}, false, nil
	}
	if err != nil {
		return invoice.Invoice{}, false, err
	}
	if storedHash != hash {
		return invoice.Invoice{}, false, errors.New("idempotency conflict")
	}
	var value invoice.Invoice
	err = json.Unmarshal([]byte(raw), &value)
	return value, true, err
}
func (s *Store) SaveReplay(ctx context.Context, tx invoiceapp.Transaction, key, hash string, value invoice.Invoice) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = sqlTx(tx).ExecContext(ctx, `INSERT INTO idempotency_records VALUES(?,?,?)`, key, hash, raw)
	return err
}
func (s *Store) Audit(ctx context.Context, tx invoiceapp.Transaction, actor security.Actor, value invoice.Invoice, decision security.Decision) error {
	_, err := sqlTx(tx).ExecContext(ctx, `INSERT INTO audit_events(actor_id,resource_id,action,policy_id,policy_version) VALUES(?,?,?,?,?)`, actor.ID, value.ID, "invoice.approve", decision.PolicyID, decision.PolicyVersion)
	return err
}
func (s *Store) Consume(ctx context.Context, tx invoiceapp.Transaction, costs []security.QuotaCost, key string, actor security.Actor) error {
	return sharedquota.ConsumeQuotas(ctx, sqlTx(tx), costs, sharedquota.Operation{ID: key, ActorID: actor.ID, ResourceType: "invoice", Action: "approve", ExpiresAt: time.Now().UTC().Add(time.Minute)})
}
