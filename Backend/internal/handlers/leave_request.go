package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LeaveRequestHandler struct {
	pool *pgxpool.Pool
}

func NewLeaveRequestHandler(pool *pgxpool.Pool) *LeaveRequestHandler {
	return &LeaveRequestHandler{pool: pool}
}

type LeaveRequestInput struct {
	EmployeeID  string `json:"employee_id" binding:"required"`
	LeaveTypeID string `json:"leave_type_id" binding:"required"`
	StartDate   string `json:"start_date" binding:"required"` // YYYY-MM-DD
	EndDate     string `json:"end_date" binding:"required"`   // YYYY-MM-DD
	Reason      string `json:"reason" binding:"required"`
}

// POST /leave-requests
func (h *LeaveRequestHandler) ApplyLeave(c *gin.Context) {
	var input LeaveRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse dates
	start, err1 := time.Parse("2006-01-02", input.StartDate)
	end, err2 := time.Parse("2006-01-02", input.EndDate)
	if err1 != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}
	if end.Before(start) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_date must be after or equal to start_date"})
		return
	}

	// Calculate working days via DB function
	var totalDays int
	err := h.pool.QueryRow(
		context.Background(),
		"SELECT calculate_working_days($1::date, $2::date)",
		start, end,
	).Scan(&totalDays)
	if err != nil || totalDays <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid leave duration"})
		return
	}

	// Check for overlapping leave requests using DB function
	var hasOverlap bool
	err = h.pool.QueryRow(
		context.Background(),
		"SELECT check_leave_overlap($1::uuid, $2::date, $3::date, NULL)",
		input.EmployeeID, start, end,
	).Scan(&hasOverlap)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check overlap"})
		return
	}
	if hasOverlap {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leave request overlaps with an existing request"})
		return
	}

	// Insert leave request
	var requestID string
	err = h.pool.QueryRow(
		context.Background(),
		`INSERT INTO leave_requests (employee_id, leave_type_id, start_date, end_date, total_days, reason, status)
		 VALUES ($1, $2, $3::date, $4::date, $5, $6, 'pending')
		 RETURNING id`,
		input.EmployeeID, input.LeaveTypeID, start, end, totalDays, input.Reason,
	).Scan(&requestID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create leave request"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Leave request submitted successfully",
		"request_id":    requestID,
		"total_days":    totalDays,
		"status":        "pending",
		"employee_id":   input.EmployeeID,
		"leave_type_id": input.LeaveTypeID,
		"start_date":    start.Format("2006-01-02"),
		"end_date":      end.Format("2006-01-02"),
	})
}
