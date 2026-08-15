package application

import (
	"context"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type Authorizer interface {
	Authorize(context.Context, domain.Request, domain.EndpointContract) (domain.Request, domain.Decision, error)
}

type Engine struct {
	hard *HardEngine
	soft *SoftEngine
}

func NewEngine(hard *HardEngine, soft *SoftEngine) *Engine {
	return &Engine{hard: hard, soft: soft}
}

func (e *Engine) Authorize(ctx context.Context, request domain.Request, contract domain.EndpointContract) (domain.Request, domain.Decision, error) {
	resolved, decision, err := e.hard.Evaluate(ctx, request, contract)
	if err != nil || !decision.Allowed {
		return resolved, decision, err
	}
	decision, err = e.soft.Decide(ctx, resolved)
	return resolved, decision, err
}

var _ Authorizer = (*Engine)(nil)
