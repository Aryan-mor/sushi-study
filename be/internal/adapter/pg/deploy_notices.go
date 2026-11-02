package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ClaimDeployNotice takes the announce slot for one service at one release tag. fresh is false when
// this service has already announced this tag: no row comes back from the conflict branch, which is
// "somebody said it already", not a failure.
func (s *Store) ClaimDeployNotice(ctx context.Context, service, tag string) (fresh bool, err error) {
	_, err = s.q.ClaimDeployNotice(ctx, ClaimDeployNoticeParams{Service: service, Tag: tag})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim deploy notice %s: %w", service, err)
	}
	return true, nil
}
