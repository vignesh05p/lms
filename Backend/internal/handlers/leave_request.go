package handlers

import (
	"context"
	"fmt"
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

	// Validate employee joining date is not after requested start date
	var joiningDate time.Time
	if err := h.pool.QueryRow(context.Background(), "SELECT joining_date FROM employees WHERE id=$1", input.EmployeeID).Scan(&joiningDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid employee_id"})
		return
	}
	if joiningDate.After(start) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date cannot be before employee's joining date"})
		return
	}

	// Ensure leave balance is available in the current year for the leave type
	var availableDays int
	currentYear := time.Now().Year()
	if err := h.pool.QueryRow(
		context.Background(),
		`SELECT available_days FROM employee_leave_balances
		 WHERE employee_id=$1 AND leave_type_id=$2 AND year=$3`,
		input.EmployeeID, input.LeaveTypeID, currentYear,
	).Scan(&availableDays); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no leave balance found for this leave type/year"})
		return
	}
	if totalDays > availableDays {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient leave balance"})
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

// GET /leave-requests/:id
func (h *LeaveRequestHandler) GetLeaveRequestByID(c *gin.Context) {
    id := c.Param("id")
    var (
        employeeID string
        leaveTypeID string
        startDate time.Time
        endDate time.Time
        totalDays int
        reason string
        status string
        appliedAt time.Time
        approvedBy *string
        approvedAt *time.Time
        rejectionReason *string
        comments *string
    )
    err := h.pool.QueryRow(
        context.Background(),
        `SELECT employee_id, leave_type_id, start_date, end_date, total_days, reason, status, applied_at, approved_by, approved_at, rejection_reason, comments
         FROM leave_requests WHERE id=$1`, id,
    ).Scan(&employeeID, &leaveTypeID, &startDate, &endDate, &totalDays, &reason, &status, &appliedAt, &approvedBy, &approvedAt, &rejectionReason, &comments)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "leave request not found"})
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "id": id,
        "employee_id": employeeID,
        "leave_type_id": leaveTypeID,
        "start_date": startDate.Format("2006-01-02"),
        "end_date": endDate.Format("2006-01-02"),
        "total_days": totalDays,
        "reason": reason,
        "status": status,
        "applied_at": appliedAt,
        "approved_by": approvedBy,
        "approved_at": approvedAt,
        "rejection_reason": rejectionReason,
        "comments": comments,
    })
}

// GET /leave-requests (optional filters: employee_id, status)
func (h *LeaveRequestHandler) ListLeaveRequests(c *gin.Context) {
    employeeID := c.Query("employee_id")
    status := c.Query("status")

    query := `SELECT id, employee_id, leave_type_id, start_date, end_date, total_days, reason, status, applied_at
              FROM leave_requests WHERE 1=1`
    args := []interface{}{}
    argIdx := 1
    if employeeID != "" {
        query += " AND employee_id=" + fmt.Sprintf("$%d", argIdx)
        args = append(args, employeeID)
        argIdx++
    }
    if status != "" {
        query += " AND status=" + fmt.Sprintf("$%d", argIdx)
        args = append(args, status)
        argIdx++
    }
    query += " ORDER BY applied_at DESC"

    rows, err := h.pool.Query(context.Background(), query, args...)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list leave requests"})
        return
    }
    defer rows.Close()

    result := make([]map[string]interface{}, 0)
    for rows.Next() {
        var (
            id string
            empID string
            ltID string
            startDate time.Time
            endDate time.Time
            totalDays int
            reason string
            statusVal string
            appliedAt time.Time
        )
        if err := rows.Scan(&id, &empID, &ltID, &startDate, &endDate, &totalDays, &reason, &statusVal, &appliedAt); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "row scan failed"})
            return
        }
        result = append(result, gin.H{
            "id": id,
            "employee_id": empID,
            "leave_type_id": ltID,
            "start_date": startDate.Format("2006-01-02"),
            "end_date": endDate.Format("2006-01-02"),
            "total_days": totalDays,
            "reason": reason,
            "status": statusVal,
            "applied_at": appliedAt,
        })
    }
    c.JSON(http.StatusOK, result)
}

// PUT /leave-requests/:id/approve
func (h *LeaveRequestHandler) ApproveLeaveRequest(c *gin.Context) {
    id := c.Param("id")
    var in struct { ApprovedBy string `json:"approved_by" binding:"required"` }
    if err := c.ShouldBindJSON(&in); err != nil || in.ApprovedBy == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "approved_by is required"})
        return
    }

    var employeeID, leaveTypeID string
    var totalDays int
    if err := h.pool.QueryRow(context.Background(), `SELECT employee_id, leave_type_id, total_days FROM leave_requests WHERE id=$1`, id).Scan(&employeeID, &leaveTypeID, &totalDays); err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "leave request not found"})
        return
    }

    tx, err := h.pool.Begin(context.Background())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "begin tx failed"})
        return
    }
    defer tx.Rollback(context.Background())

    if _, err := tx.Exec(context.Background(),
        `UPDATE leave_requests SET status='approved', approved_by=$1, approved_at=NOW() WHERE id=$2`, in.ApprovedBy, id,
    ); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve request"})
        return
    }

    currentYear := time.Now().Year()
    if _, err := tx.Exec(context.Background(),
        `UPDATE employee_leave_balances SET used_days = used_days + $1 WHERE employee_id=$2 AND leave_type_id=$3 AND year=$4`,
        totalDays, employeeID, leaveTypeID, currentYear,
    ); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update leave balance"})
        return
    }

    if err := tx.Commit(context.Background()); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "leave request approved"})
}

// PUT /leave-requests/:id/reject
func (h *LeaveRequestHandler) RejectLeaveRequest(c *gin.Context) {
    id := c.Param("id")
    var in struct { RejectionReason string `json:"rejection_reason" binding:"required"` }
    if err := c.ShouldBindJSON(&in); err != nil || in.RejectionReason == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "rejection_reason is required"})
        return
    }
    if _, err := h.pool.Exec(context.Background(),
        `UPDATE leave_requests SET status='rejected', rejection_reason=$1 WHERE id=$2`, in.RejectionReason, id,
    ); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject request"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "leave request rejected"})
}

// PUT /leave-requests/:id/cancel
func (h *LeaveRequestHandler) CancelLeaveRequest(c *gin.Context) {
    id := c.Param("id")
    if _, err := h.pool.Exec(context.Background(),
        `UPDATE leave_requests SET status='cancelled' WHERE id=$1`, id,
    ); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel request"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "leave request cancelled"})
}
