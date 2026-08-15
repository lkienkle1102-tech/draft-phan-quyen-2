package application

import (
	"context"
	"errors"
	"time"

	"example.com/phan-quyen-golang/internal/security/domain"
)

var ErrInvalidExternalGrant = errors.New("invalid external grant")
var ErrExternalGrantForbidden = errors.New("external grant management forbidden")
var ErrGrantEscalation = errors.New("organization grant exceeds owner entitlements")

type ExternalGrantStore interface {
	CreateExternalGrant(context.Context, domain.ExternalGrantDefinition) error
	RevokeExternalGrant(context.Context, string, string, string, time.Time) error
	ListExternalGrants(context.Context, string) ([]domain.ExternalGrantSummary, error)
}
type PermissionCatalog interface {
	PermissionOperation(context.Context, string) (domain.Operation, error)
}

func (s *ExternalGrantService) List(ctx context.Context, owner, actor string) ([]domain.ExternalGrantSummary, error) {
	subject := domain.Subject{Type: domain.SubjectOrganization, ID: owner}
	a := domain.Actor{ID: actor, Type: domain.ActorUser}
	member, err := s.facts.IsMember(ctx, a, subject)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrExternalGrantForbidden
	}
	allowed, err := s.permissions.Enforce(ctx, a, subject, domain.Operation{ResourceType: "external_grant", Action: "manage"})
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrExternalGrantForbidden
	}
	return s.store.ListExternalGrants(ctx, owner)
}

type ExternalGrantService struct {
	facts       BusinessFacts
	permissions PermissionEnforcer
	directory   AuthorizationDirectory
	store       ExternalGrantStore
}

func NewExternalGrantService(f BusinessFacts, permissions PermissionEnforcer, directory AuthorizationDirectory, s ExternalGrantStore) *ExternalGrantService {
	return &ExternalGrantService{facts: f, permissions: permissions, directory: directory, store: s}
}

func (s *ExternalGrantService) Create(ctx context.Context, definition domain.ExternalGrantDefinition) error {
	if err := validateExternalGrant(definition); err != nil {
		return err
	}
	owner := domain.Subject{Type: domain.SubjectOrganization, ID: definition.OwnerOrganizationID}
	actor := domain.Actor{ID: definition.CreatedBy, Type: domain.ActorUser}
	if err := s.requireManager(ctx, actor, owner); err != nil {
		return err
	}
	if err := s.checkPermissions(ctx, actor, owner, definition.Permissions); err != nil {
		return err
	}
	if err := s.directory.ValidateRoles(ctx, owner, itemKeys(definition.Roles)); err != nil {
		return errors.Join(ErrInvalidExternalGrant, err)
	}
	if err := s.directory.ValidateGroups(ctx, owner, itemKeys(definition.Groups)); err != nil {
		return errors.Join(ErrInvalidExternalGrant, err)
	}
	if err := checkExternalNamed(ctx, owner, definition.Features, s.facts.HasFeature); err != nil {
		return err
	}
	if err := checkExternalNamed(ctx, owner, definition.Plans, s.facts.HasPlan); err != nil {
		return err
	}
	if err := s.checkExternalQuotas(ctx, owner, definition.Quotas); err != nil {
		return err
	}
	return s.store.CreateExternalGrant(ctx, definition)
}

func itemKeys(items []domain.ExternalGrantItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Key)
	}
	return result
}

func (s *ExternalGrantService) requireManager(ctx context.Context, actor domain.Actor, owner domain.Subject) error {
	member, err := s.facts.IsMember(ctx, actor, owner)
	if err != nil {
		return err
	}
	if !member {
		return ErrExternalGrantForbidden
	}
	allowed, err := s.permissions.Enforce(ctx, actor, owner, domain.Operation{ResourceType: "external_grant", Action: "manage"})
	if err != nil {
		return err
	}
	if !allowed {
		return ErrExternalGrantForbidden
	}
	return nil
}
func (s *ExternalGrantService) checkPermissions(ctx context.Context, actor domain.Actor, owner domain.Subject, items []domain.ExternalGrantItem) error {
	catalog, supported := s.store.(PermissionCatalog)
	if !supported {
		return ErrInvalidExternalGrant
	}
	for _, item := range items {
		if item.Effect != domain.EffectAllow {
			continue
		}
		operation, loadErr := catalog.PermissionOperation(ctx, item.Key)
		if loadErr != nil {
			return loadErr
		}
		owned, loadErr := s.permissions.Enforce(ctx, actor, owner, operation)
		if loadErr != nil {
			return loadErr
		}
		if !owned {
			return ErrGrantEscalation
		}
	}
	return nil
}

type subjectNamedFact func(context.Context, domain.Subject, string) (bool, error)

func checkExternalNamed(ctx context.Context, owner domain.Subject, items []domain.ExternalGrantItem, check subjectNamedFact) error {
	for _, item := range items {
		if item.Effect == domain.EffectAllow {
			ok, e := check(ctx, owner, item.Key)
			if e != nil {
				return e
			}
			if !ok {
				return ErrGrantEscalation
			}
		}
	}
	return nil
}
func (s *ExternalGrantService) checkExternalQuotas(ctx context.Context, owner domain.Subject, items []domain.ExternalGrantItem) error {
	for _, item := range items {
		if item.Effect == domain.EffectAllow && item.Limit != nil {
			ok, e := s.facts.QuotaAvailable(ctx, owner, item.Key, *item.Limit)
			if e != nil {
				return e
			}
			if !ok {
				return ErrGrantEscalation
			}
		}
	}
	return nil
}
func (s *ExternalGrantService) Revoke(ctx context.Context, owner, grant, actor string, at time.Time) error {
	subject := domain.Subject{Type: domain.SubjectOrganization, ID: owner}
	a := domain.Actor{ID: actor, Type: domain.ActorUser}
	member, err := s.facts.IsMember(ctx, a, subject)
	if err != nil {
		return err
	}
	if !member {
		return ErrExternalGrantForbidden
	}
	allowed, err := s.permissions.Enforce(ctx, a, subject, domain.Operation{ResourceType: "external_grant", Action: "manage"})
	if err != nil {
		return err
	}
	if !allowed {
		return ErrExternalGrantForbidden
	}
	return s.store.RevokeExternalGrant(ctx, owner, grant, actor, at)
}

func validateExternalGrant(g domain.ExternalGrantDefinition) error {
	if g.ID == "" || g.OwnerOrganizationID == "" || g.CreatedBy == "" || g.Resource.Type == "" || g.Operation.Action == "" || g.Operation.ResourceType != g.Resource.Type || g.ValidFrom.IsZero() || !validEffect(g.Effect) {
		return ErrInvalidExternalGrant
	}
	if g.ValidUntil != nil && !g.ValidUntil.After(g.ValidFrom) {
		return ErrInvalidExternalGrant
	}
	if !validExternalTarget(g.Target) || !validExternalItems(g) {
		return ErrInvalidExternalGrant
	}
	return nil
}
func validEffect(effect domain.Effect) bool {
	return effect == domain.EffectAllow || effect == domain.EffectDeny
}
func validExternalTarget(target domain.ExternalGrantTarget) bool {
	switch target.Type {
	case domain.ExternalTargetGlobalUser:
		return target.UserID != "" && target.OrganizationID == "" && target.MembershipID == ""
	case domain.ExternalTargetOrganization:
		return target.OrganizationID != "" && target.UserID == "" && target.MembershipID == ""
	case domain.ExternalTargetOrganizationMember:
		return target.OrganizationID != "" && target.UserID != "" && target.MembershipID != ""
	default:
		return false
	}
}
func validExternalItems(g domain.ExternalGrantDefinition) bool {
	for _, items := range [][]domain.ExternalGrantItem{g.Permissions, g.Roles, g.Groups, g.Features, g.Plans, g.Quotas} {
		for _, item := range items {
			if item.Key == "" || !validEffect(item.Effect) || (item.Limit != nil && *item.Limit < 0) {
				return false
			}
		}
	}
	return true
}
