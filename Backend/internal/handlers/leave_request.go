package handlers

import (
	"context"
	"context"
	"fmt"
	"net/http"
	"time"

	"leave-management/internal/models"

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
	var input struct {
		LeaveTypeID string `json:"leave_type_id" binding:"required"`
		StartDate   string `json:"start_date" binding:"required"`
		EndDate     string `json:"end_date" binding:"required"`
		Reason      string `json:"reason" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Get authenticated user's employee ID
	employeeID, exists := c.Get("employee_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Parse dates
	start, err := time.Parse("2006-01-02", input.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format, use YYYY-MM-DD"})
		return
	}

	end, err := time.Parse("2006-01-02", input.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format, use YYYY-MM-DD"})
		return
	}

	// Validate dates
	if start.After(end) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date cannot be after end_date"})
		return
	}

	// Validate employee joining date is not after requested start date
	var joiningDate time.Time
	if err := h.pool.QueryRow(context.Background(), "SELECT joining_date FROM employees WHERE id=$1", employeeID).Scan(&joiningDate); err != nil {
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
		employeeID, input.LeaveTypeID, currentYear,
	).Scan(&availableDays); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no leave balance found for this leave type/year"})
		return
	}

	// Calculate total days
	totalDays := int(end.Sub(start).Hours()/24) + 1

	if totalDays > availableDays {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient leave balance"})
		return
	}

	// Check for overlapping leave requests
	var hasOverlap bool
	if err := h.pool.QueryRow(
		context.Background(),
		"SELECT check_leave_overlap($1, $2, $3, NULL)",
		employeeID, start, end,
	).Scan(&hasOverlap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check leave overlap"})
		return
	}

	if hasOverlap {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leave request overlaps with an existing request"})
		return
	}

	// Insert leave request
	var requestID string
	if err := h.pool.QueryRow(
		context.Background(),
		`INSERT INTO leave_requests (employee_id, leave_type_id, start_date, end_date, total_days, reason, status, applied_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW(), NOW(), NOW())
		 RETURNING id`,
		employeeID, input.LeaveTypeID, start, end, totalDays, input.Reason,
	).Scan(&requestID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create leave request", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Leave request created successfully",
		"request_id": requestID,
		"total_days": totalDays,
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
	// Get user context from middleware
	userID, _ := c.Get("user_id")
	userRole, _ := c.Get("role")
	employeeID, _ := c.Get("employee_id")

	// Build query based on user role
	var query string
	var args []interface{}
	argIdx := 1

	switch userRole.(string) {
	case models.RoleAdmin, models.RoleHR:
		// Admin and HR can see all requests
		query = `SELECT lr.id, lr.employee_id, lr.leave_type_id, lr.start_date, lr.end_date, 
			lr.total_days, lr.reason, lr.status, lr.applied_at, lr.approved_by, lr.approved_at, 
			lr.rejection_reason, lr.comments, lr.created_at, lr.updated_at,
			e.name as employee_name, e.email as employee_email,
			lt.name as leave_type_name
			FROM leave_requests lr
			JOIN employees e ON lr.employee_id = e.id
			JOIN leave_types lt ON lr.leave_type_id = lt.id
			WHERE 1=1`

	case models.RoleManager:
		// Managers can see their team's requests
		query = `SELECT lr.id, lr.employee_id, lr.leave_type_id, lr.start_date, lr.end_date, 
			lr.total_days, lr.reason, lr.status, lr.applied_at, lr.approved_by, lr.approved_at, 
			lr.rejection_reason, lr.comments, lr.created_at, lr.updated_at,
			e.name as employee_name, e.email as employee_email,
			lt.name as leave_type_name
			FROM leave_requests lr
			JOIN employees e ON lr.employee_id = e.id
			JOIN leave_types lt ON lr.leave_type_id = lt.id
			WHERE e.manager_id = $` + fmt.Sprint(argIdx)
		args = append(args, userID)
		argIdx++

	case models.RoleEmployee:
		// Employees can only see their own requests
		query = `SELECT lr.id, lr.employee_id, lr.leave_type_id, lr.start_date, lr.end_date, 
			lr.total_days, lr.reason, lr.status, lr.applied_at, lr.approved_by, lr.approved_at, 
			lr.rejection_reason, lr.comments, lr.created_at, lr.updated_at,
			e.name as employee_name, e.email as employee_email,
			lt.name as leave_type_name
			FROM leave_requests lr
			JOIN employees e ON lr.employee_id = e.id
			JOIN leave_types lt ON lr.leave_type_id = lt.id
			WHERE lr.employee_id = $` + fmt.Sprint(argIdx)
		args = append(args, employeeID)
		argIdx++
	}

	// Add filters
	if status := c.Query("status"); status != "" {
		query += " AND lr.status = $" + fmt.Sprint(argIdx)
		args = append(args, status)
		argIdx++
	}

	// For Admin/HR, allow filtering by employee_id
	if userRole.(string) == models.RoleAdmin || userRole.(string) == models.RoleHR {
		if employeeIDFilter := c.Query("employee_id"); employeeIDFilter != "" {
			query += " AND lr.employee_id = $" + fmt.Sprint(argIdx)
			args = append(args, employeeIDFilter)
			argIdx++
		}
	}

	query += " ORDER BY lr.created_at DESC"

	rows, err := h.pool.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch leave requests", "details": err.Error()})
		return
	}
	defer rows.Close()

	var requests []map[string]interface{}
	for rows.Next() {
		var (
			id              string
			empID           string
			leaveTypeID     string
			startDate       time.Time
			endDate         time.Time
			totalDays       int
			reason          string
			status          string
			appliedAt       time.Time
			approvedBy      *string
			approvedAt      *time.Time
			rejectionReason *string
			comments        *string
			createdAt       time.Time
			updatedAt       time.Time
			employeeName    string
			employeeEmail   string
			leaveTypeName   string
		)

		if err := rows.Scan(&id, &empID, &leaveTypeID, &startDate, &endDate, &totalDays, &reason, &status, &appliedAt, &approvedBy, &approvedAt, &rejectionReason, &comments, &createdAt, &updatedAt, &employeeName, &employeeEmail, &leaveTypeName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan leave request", "details": err.Error()})
			return
		}

		request := gin.H{
			"id":              id,
			"employee_id":     empID,
			"leave_type_id":   leaveTypeID,
			"start_date":      startDate.Format("2006-01-02"),
			"end_date":        endDate.Format("2006-01-02"),
			"total_days":      totalDays,
			"reason":          reason,
			"status":          status,
			"applied_at":      appliedAt,
			"approved_by":     approvedBy,
			"approved_at":     approvedAt,
			"rejection_reason": rejectionReason,
			"comments":        comments,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
			"employee_name":   employeeName,
			"employee_email":  employeeEmail,
			"leave_type_name": leaveTypeName,
		}
		requests = append(requests, request)
	}

	c.JSON(http.StatusOK, requests)
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
