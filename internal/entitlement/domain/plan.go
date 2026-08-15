// Package domain defines plan, feature, and quota entitlement concepts.
package domain

import (
	"errors"
	"time"
)

type SubjectType string

const (
	SubjectUser         SubjectType = "user"
	SubjectOrganization SubjectType = "organization"
)

type Subject struct {
	Type SubjectType
	ID   string
}

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

type SubscriptionStatus string

const (
	SubscriptionTrialing  SubscriptionStatus = "trialing"
	SubscriptionActive    SubscriptionStatus = "active"
	SubscriptionCancelled SubscriptionStatus = "cancelled"
)

type Subscription struct {
	ID, PlanID             string
	Subject                Subject
	Effect                 Effect
	Status                 SubscriptionStatus
	ValidFrom              time.Time
	ValidUntil             *time.Time
	PeriodStart, PeriodEnd time.Time
}

func (s Subscription) Validate() error {
	if s.ID == "" || s.PlanID == "" || s.Subject.ID == "" || s.ValidFrom.IsZero() || s.PeriodStart.IsZero() || !s.PeriodEnd.After(s.PeriodStart) {
		return errors.New("invalid subscription")
	}
	if s.Effect != EffectAllow && s.Effect != EffectDeny {
		return errors.New("invalid subscription effect")
	}
	if s.Status != SubscriptionTrialing && s.Status != SubscriptionActive && s.Status != SubscriptionCancelled {
		return errors.New("invalid subscription status")
	}
	if s.ValidUntil != nil && !s.ValidUntil.After(s.ValidFrom) {
		return errors.New("invalid subscription validity")
	}
	return nil
}

type Override struct {
	ID                     string
	Subject                Subject
	Key                    string
	Effect                 Effect
	Limit                  *int64
	ValidFrom              time.Time
	ValidUntil             *time.Time
	PeriodStart, PeriodEnd *time.Time
}

func (o Override) Validate() error {
	if o.ID == "" || o.Subject.ID == "" || o.Key == "" || o.ValidFrom.IsZero() {
		return errors.New("invalid entitlement override")
	}
	if o.Effect != EffectAllow && o.Effect != EffectDeny {
		return errors.New("invalid entitlement effect")
	}
	if o.Limit != nil && *o.Limit < 0 {
		return errors.New("negative quota limit")
	}
	if o.ValidUntil != nil && !o.ValidUntil.After(o.ValidFrom) {
		return errors.New("invalid entitlement validity")
	}
	return nil
}
