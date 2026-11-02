package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/dailystats"
)

// DailyStats reads the superadmin digest snapshot. One statement, four counts.
func (s *Store) DailyStats(ctx context.Context) (dailystats.Snapshot, error) {
	row, err := s.q.DailyStatsSnapshot(ctx)
	if err != nil {
		return dailystats.Snapshot{}, fmt.Errorf("daily stats: %w", err)
	}
	return dailystats.Snapshot{
		Online24h:    row.Online24h,
		Registered:   row.Registered,
		Delivered24h: row.Delivered24h,
		Indexed:      row.Indexed,
	}, nil
}

// ClaimDailyStatsSend takes today's local YMD slot. fresh is false when this day was already sent.
func (s *Store) ClaimDailyStatsSend(ctx context.Context, ymd string) (bool, error) {
	var day pgtype.Date
	if err := day.Scan(ymd); err != nil {
		return false, fmt.Errorf("claim daily stats %s: %w", ymd, err)
	}
	_, err := s.q.ClaimDailyStatsSend(ctx, day)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim daily stats %s: %w", ymd, err)
	}
	return true, nil
}
