// Package application exposes the identity snapshot use case.
package application

import (
	"context"
	"errors"
	"time"

	identity "example.com/phan-quyen-golang/internal/identity/domain"
	security "example.com/phan-quyen-golang/internal/security/domain"
)

var ErrUserRequired = errors.New("user actor required")

type SnapshotReader interface {
	Read(context.Context, security.Actor, time.Time) (identity.Snapshot, error)
}

type Service struct {
	reader SnapshotReader
	now    func() time.Time
}

func NewService(reader SnapshotReader) *Service {
	return &Service{reader: reader, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) GetMe(ctx context.Context, actor security.Actor) (identity.Snapshot, error) {
	if actor.Type != security.ActorUser {
		return identity.Snapshot{}, ErrUserRequired
	}
	return s.reader.Read(ctx, actor, s.now())
}
