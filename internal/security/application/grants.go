package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

var ErrGrantEscalation = errors.New("organization grant exceeds owner entitlements")

type GrantCommand struct {
	ID, OwnerOrganizationID, GranteeOrganizationID, GranteeUserID, CreatedBy string
	Resource                                                                 domain.Resource
	Operation                                                                domain.Operation
	ValidFrom                                                                time.Time
	ValidUntil                                                               *time.Time
	Features                                                                 []string
	Quotas                                                                   map[string]int64
}
type GrantWriter interface {
	CreateGrant(context.Context, GrantCommand) error
}
type GrantService struct {
	facts  Facts
	writer GrantWriter
}

func NewGrantService(facts Facts, writer GrantWriter) *GrantService {
	return &GrantService{facts: facts, writer: writer}
}
func (s *GrantService) Create(ctx context.Context, command GrantCommand) error {
	if command.OwnerOrganizationID == command.GranteeOrganizationID || command.Resource.System {
		return ErrGrantEscalation
	}
	subject := domain.Subject{Type: domain.SubjectOrganization, ID: command.OwnerOrganizationID}
	actor := domain.Actor{ID: command.CreatedBy, Type: domain.ActorUser}
	member, err := s.facts.IsMember(ctx, actor, subject)
	if err != nil || !member {
		return errors.Join(ErrGrantEscalation, err)
	}
	if command.GranteeUserID != "" {
		grantee := domain.Actor{ID: command.GranteeUserID, Type: domain.ActorUser}
		granteeSubject := domain.Subject{Type: domain.SubjectOrganization, ID: command.GranteeOrganizationID}
		member, err = s.facts.IsMember(ctx, grantee, granteeSubject)
		if err != nil || !member {
			return errors.Join(ErrGrantEscalation, err)
		}
	}
	allowed, err := s.facts.HasPermission(ctx, actor, subject, command.Operation)
	if err != nil || !allowed {
		return errors.Join(ErrGrantEscalation, err)
	}
	if err := s.checkFeatures(ctx, subject, command.Features); err != nil {
		return err
	}
	if err := s.checkQuotas(ctx, subject, command.Quotas); err != nil {
		return err
	}
	return s.writer.CreateGrant(ctx, command)
}
func (s *GrantService) checkFeatures(ctx context.Context, subject domain.Subject, features []string) error {
	for _, feature := range features {
		allowed, err := s.facts.HasFeature(ctx, subject, feature)
		if err != nil || !allowed {
			return errors.Join(ErrGrantEscalation, err)
		}
	}
	return nil
}
func (s *GrantService) checkQuotas(ctx context.Context, subject domain.Subject, quotas map[string]int64) error {
	for key, amount := range quotas {
		allowed, err := s.facts.QuotaAvailable(ctx, subject, key, amount)
		if err != nil || !allowed {
			return errors.Join(ErrGrantEscalation, err)
		}
	}
	return nil
}
