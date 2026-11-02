package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// requestPathParams are the guards doc 06 §7 asks for: no single query, and no single forgotten
// transaction, may hold a database hostage.
//
// They belong on the connection rather than on the sushi_app role, which is where they used to be
// (db/bootstrap.sql). That role is also what runs goose and fetcherd, so a role-wide
// `statement_timeout = 10s` aborted any migration that built a real index -- the trigram index over
// items.title takes considerably longer than ten seconds on a full catalog -- and cut fetcherd's
// whole-catalog reads off mid-sync; `lock_timeout = 3s` would drop any ALTER TABLE that met a live
// reader. Set here, they constrain exactly the workload they were written for and nothing else.
//
// Postgres applies these from the startup packet, so every connection the pool opens has them
// before it runs anything.
var requestPathParams = map[string]string{
	"statement_timeout":                   "10s",
	"idle_in_transaction_session_timeout": "30s",
	"lock_timeout":                        "3s",
}

// OpenRequestPath connects a pool for a daemon that answers user requests -- apid, deliveryd,
// main-bot, init-bot -- with the limits above applied.
//
// maxConns of 0 leaves pgx's default. Background jobs (fetcherd, backupd) and goose deliberately do
// not use this: their work is supposed to take minutes.
func OpenRequestPath(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	for name, value := range requestPathParams {
		cfg.ConnConfig.RuntimeParams[name] = value
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
