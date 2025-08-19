package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type LeaveType struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	MaxDaysPerYear      int     `json:"max_days_per_year"`
	CarryForwardAllowed bool    `json:"carry_forward_allowed"`
	MaxCarryForwardDays int     `json:"max_carry_forward_days"`
	IsActive            bool    `json:"is_active"`
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env: %s", key)
	}
	return v
}

func connectDB(ctx context.Context, url string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatalf("parse db url: %v", err)
	}
	// reasonable pool defaults
	cfg.MaxConns = 5
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("db ping failed: %v", err)
	}
	return pool
}

func main() {
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := mustGetEnv("DATABASE_URL")

	ctx := context.Background()
	pool := connectDB(ctx, dbURL)
	defer pool.Close()

	r := gin.Default()

	// Simple health check (also verifies DB connectivity)
	r.GET("/health", func(c *gin.Context) {
		var one int
		if err := pool.QueryRow(ctx, "select 1").Scan(&one); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "db_error", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Our first real endpoint: list active leave types
	r.GET("/leave-types", func(c *gin.Context) {
		rows, err := pool.Query(ctx, `
			select id::text, name, description, max_days_per_year,
			       carry_forward_allowed, max_carry_forward_days, is_active
			from leave_types
			where is_active = true
			order by name asc
		`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query error", "details": err.Error()})
			return
		}
		defer rows.Close()

		var list []LeaveType
		for rows.Next() {
			var lt LeaveType
			if err := rows.Scan(&lt.ID, &lt.Name, &lt.Description, &lt.MaxDaysPerYear, &lt.CarryForwardAllowed, &lt.MaxCarryForwardDays, &lt.IsActive); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan error", "details": err.Error()})
				return
			}
			list = append(list, lt)
		}
		if rows.Err() != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rows error", "details": rows.Err().Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	})

	log.Printf("listening on :%s ...", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
