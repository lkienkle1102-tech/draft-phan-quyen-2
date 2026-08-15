package application

import (
	"context"

	"example.com/phan-quyen-golang/internal/security/domain"
)

type contextKey uint8

const (
	actorContextKey contextKey = iota + 1
	decisionContextKey
)

func WithActor(ctx context.Context, actor domain.Actor) context.Context {
	return context.WithValue(ctx, actorContextKey, actor)
}
func ActorFromContext(ctx context.Context) (domain.Actor, bool) {
	value, ok := ctx.Value(actorContextKey).(domain.Actor)
	return value, ok
}
func WithDecision(ctx context.Context, decision domain.Decision) context.Context {
	return context.WithValue(ctx, decisionContextKey, decision)
}
func DecisionFromContext(ctx context.Context) (domain.Decision, bool) {
	value, ok := ctx.Value(decisionContextKey).(domain.Decision)
	return value, ok
}
