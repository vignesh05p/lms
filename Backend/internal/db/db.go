package db

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatalf("parse db url: %v", err)
	}

	// Set session defaults for every new connection in the pool.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Mark app name (optional) and set Supabase RLS claims.
		// We need at least role=admin to pass your RLS insert policies.
		_, err := conn.Exec(ctx, `
			SET application_name = 'lms-backend';
			SET "request.jwt.claims" = '{"role":"admin"}';
		`)
		return err
	}

	// Reasonable pool sizes for dev
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	// simple ping
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("db ping failed: %v", err)
	}
	return pool
}
