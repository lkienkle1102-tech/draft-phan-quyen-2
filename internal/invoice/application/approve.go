// Package application implements invoice use cases.
package application

import (
	"context"
	"errors"

	invoice "example.com/phan-quyen-golang/internal/invoice/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

var (
	ErrUnknownStrategy   = errors.New("unknown invoice strategy")
	ErrUnknownObligation = errors.New("unknown invoice obligation")
)

type Transaction interface{ TransactionMarker() }
type UnitOfWork interface {
	Within(context.Context, func(Transaction) error) error
}
type Repository interface {
	Find(context.Context, Transaction, string) (invoice.Invoice, error)
	Approve(context.Context, Transaction, string, int64) (invoice.Invoice, error)
	RequestManualReview(context.Context, Transaction, string, int64) (invoice.Invoice, error)
	Replay(context.Context, Transaction, string, string) (invoice.Invoice, bool, error)
	SaveReplay(context.Context, Transaction, string, string, invoice.Invoice) error
	Audit(context.Context, Transaction, security.Actor, invoice.Invoice, security.Decision) error
}
type Quotas interface {
	Consume(context.Context, Transaction, []security.QuotaCost, string, security.Actor) error
}
type Command struct {
	Actor                       security.Actor
	InvoiceID                   string
	Version                     int64
	IdempotencyKey, RequestHash string
}
type ApproveService struct {
	work       UnitOfWork
	repository Repository
	quotas     Quotas
}

func NewApproveService(work UnitOfWork, repository Repository, quotas Quotas) *ApproveService {
	return &ApproveService{work: work, repository: repository, quotas: quotas}
}

func (s *ApproveService) Execute(ctx context.Context, command Command, decision security.Decision) (invoice.Invoice, error) {
	wanted := security.Operation{ResourceType: "invoice", Action: "approve"}
	if !decision.Allowed || decision.Operation != wanted {
		return invoice.Invoice{}, errors.New("invalid authorization decision")
	}
	var result invoice.Invoice
	err := s.work.Within(ctx, func(tx Transaction) error {
		value, err := s.execute(ctx, tx, command, decision)
		result = value
		return err
	})
	return result, err
}

func (s *ApproveService) execute(ctx context.Context, tx Transaction, command Command, decision security.Decision) (invoice.Invoice, error) {
	replay, found, err := s.repository.Replay(ctx, tx, command.IdempotencyKey, command.RequestHash)
	if err != nil || found {
		return replay, err
	}
	current, err := s.repository.Find(ctx, tx, command.InvoiceID)
	if err != nil {
		return current, err
	}
	if err := invoice.CanApprove(command.Actor.ID, current); err != nil {
		return current, err
	}
	strategy, err := invoiceStrategy(decision)
	if err != nil {
		return current, err
	}
	if strategy == "manual_review" {
		result, err := s.repository.RequestManualReview(ctx, tx, current.ID, command.Version)
		if err != nil {
			return result, err
		}
		if err := s.repository.Audit(ctx, tx, command.Actor, result, decision); err != nil {
			return result, err
		}
		return result, s.repository.SaveReplay(ctx, tx, command.IdempotencyKey, command.RequestHash, result)
	}
	if strategy != "" && strategy != "approve" {
		return current, ErrUnknownStrategy
	}
	if err := s.quotas.Consume(ctx, tx, decision.QuotaCosts, command.IdempotencyKey, command.Actor); err != nil {
		return current, err
	}
	result, err := s.repository.Approve(ctx, tx, current.ID, command.Version)
	if err != nil {
		return result, err
	}
	if err := s.repository.Audit(ctx, tx, command.Actor, result, decision); err != nil {
		return result, err
	}
	return result, s.repository.SaveReplay(ctx, tx, command.IdempotencyKey, command.RequestHash, result)
}

func invoiceStrategy(decision security.Decision) (string, error) {
	strategy := decision.Strategy
	for _, obligation := range decision.Obligations {
		switch obligation.Type {
		case "require_manual_review":
			strategy = "manual_review"
		default:
			return "", ErrUnknownObligation
		}
	}
	return strategy, nil
}
