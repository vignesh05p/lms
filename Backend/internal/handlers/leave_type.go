package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LeaveTypeHandler struct {
	pool *pgxpool.Pool
}

func NewLeaveTypeHandler(pool *pgxpool.Pool) *LeaveTypeHandler {
	return &LeaveTypeHandler{pool: pool}
}

// GET /leave-types
func (h *LeaveTypeHandler) GetLeaveTypes(c *gin.Context) {
	rows, err := h.pool.Query(context.Background(), "SELECT id, name, description, max_days_per_year FROM leave_types WHERE is_active = TRUE")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch leave types"})
		return
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, name, desc string
		var maxDays int
		if err := rows.Scan(&id, &name, &desc, &maxDays); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "row scan failed"})
			return
		}
		result = append(result, gin.H{
			"id":                id,
			"name":              name,
			"description":       desc,
			"max_days_per_year": maxDays,
		})
	}

	c.JSON(http.StatusOK, result)
}
