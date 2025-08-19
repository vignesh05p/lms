package router

import (
	"leave-management/internal/handlers"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Setup(r *gin.Engine, pool *pgxpool.Pool) {
	eh := handlers.NewEmployeeHandler(pool)

	// health
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	// first API: create employee
	r.POST("/employees", eh.CreateEmployee)
}
