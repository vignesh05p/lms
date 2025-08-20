package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EmployeeHandler struct {
	Pool *pgxpool.Pool
}

func NewEmployeeHandler(pool *pgxpool.Pool) *EmployeeHandler {
	return &EmployeeHandler{Pool: pool}
}

type createEmployeeDTO struct {
	Name         string `json:"name" binding:"required"`
	Email        string `json:"email" binding:"required"`
	DepartmentID string `json:"department_id" binding:"required"`
	JoiningDate  string `json:"joining_date" binding:"required"` // "YYYY-MM-DD"
	EmployeeID   string `json:"employee_id"`                     // optional
}

func (h *EmployeeHandler) CreateEmployee(c *gin.Context) {
	var in createEmployeeDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input", "details": err.Error()})
		return
	}

	// Basic validations
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.Name == "" || in.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and email are required"})
		return
	}
	joinDate, err := time.Parse("2006-01-02", in.JoiningDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "joining_date must be YYYY-MM-DD"})
		return
	}
	if joinDate.After(time.Now().Truncate(24 * time.Hour)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "joining_date cannot be in the future"})
		return
	}
	empID := strings.TrimSpace(in.EmployeeID)
	if empID == "" {
		empID = generateEmployeeID()
	}

	ctx := context.Background()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "begin tx failed", "details": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	// 1) Ensure department exists
	var depExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM departments WHERE id=$1)`, in.DepartmentID).
		Scan(&depExists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "dept check failed", "details": err.Error()})
		return
	}
	if !depExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "department_id not found"})
		return
	}

	// 2) Insert employee (RLS requires role in ('hr','admin') -> set via db.AfterConnect)
	var newID string
	err = tx.QueryRow(ctx, `
		INSERT INTO employees (employee_id, email, name, department_id, joining_date, role)
		VALUES ($1, $2, $3, $4, $5, 'employee')
		RETURNING id
	`, empID, in.Email, in.Name, in.DepartmentID, joinDate).Scan(&newID)
	if err != nil {
		msg := parsePgErr(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "insert employee failed", "details": msg})
		return
	}

	// 3) Allocate current-year leave balances for all active leave types
	year := time.Now().Year()
	_, err = tx.Exec(ctx, `
		INSERT INTO employee_leave_balances (employee_id, leave_type_id, year, allocated_days, used_days, carried_forward_days)
		SELECT $1, lt.id, $2, lt.max_days_per_year, 0, 0
		FROM leave_types lt
		WHERE lt.is_active = true
		ON CONFLICT (employee_id, leave_type_id, year) DO NOTHING
	`, newID, year)
	if err != nil {
		msg := parsePgErr(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "allocate leave balances failed", "details": msg})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":            newID,
		"employee_id":   empID,
		"name":          in.Name,
		"email":         in.Email,
		"department_id": in.DepartmentID,
		"joining_date":  joinDate.Format("2006-01-02"),
		"message":       "Employee added successfully",
	})
}

func generateEmployeeID() string {
	// Simple random ID like EMP-2025-xxxxx
	return "EMP-" + time.Now().Format("20060102-150405")
}

func parsePgErr(err error) string {
	// Keep it simple; surface useful messages for common constraints.
	msg := err.Error()
	// helpful hints
	if strings.Contains(msg, "employees_email_key") {
		return "email already exists"
	}
	if strings.Contains(msg, "employees_employee_id_key") {
		return "employee_id already exists"
	}
	if strings.Contains(msg, "check_joining_date") {
		return "joining_date cannot be in the future"
	}
	if strings.Contains(strings.ToLower(msg), "violates row-level security") {
		return "RLS blocked the operation (ensure admin/hr role is set in DB session)"
	}
	return msg
}

// GET /employees/:id/leave-balances
func (h *EmployeeHandler) GetLeaveBalances(c *gin.Context) {
	employeeID := c.Param("id")
	
	// Validate employee exists
	var employeeName string
	if err := h.Pool.QueryRow(context.Background(), "SELECT name FROM employees WHERE id=$1", employeeID).Scan(&employeeName); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "employee not found"})
		return
	}

	// Get leave balances for current year
	currentYear := time.Now().Year()
	rows, err := h.Pool.Query(context.Background(), `
		SELECT 
			lt.id as leave_type_id,
			lt.name as leave_type_name,
			lt.description as leave_type_description,
			elb.allocated_days,
			elb.used_days,
			elb.carried_forward_days,
			elb.available_days,
			elb.year
		FROM employee_leave_balances elb
		JOIN leave_types lt ON elb.leave_type_id = lt.id
		WHERE elb.employee_id = $1 AND elb.year = $2
		ORDER BY lt.name
	`, employeeID, currentYear)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch leave balances"})
		return
	}
	defer rows.Close()

	var balances []map[string]interface{}
	for rows.Next() {
		var (
			leaveTypeID          string
			leaveTypeName        string
			leaveTypeDescription string
			allocatedDays        int
			usedDays             int
			carriedForwardDays   int
			availableDays        int
			year                 int
		)
		if err := rows.Scan(&leaveTypeID, &leaveTypeName, &leaveTypeDescription, &allocatedDays, &usedDays, &carriedForwardDays, &availableDays, &year); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "row scan failed"})
			return
		}
		balances = append(balances, gin.H{
			"leave_type_id":          leaveTypeID,
			"leave_type_name":        leaveTypeName,
			"leave_type_description": leaveTypeDescription,
			"allocated_days":         allocatedDays,
			"used_days":              usedDays,
			"carried_forward_days":   carriedForwardDays,
			"available_days":         availableDays,
			"year":                   year,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"employee_id": employeeID,
		"employee_name": employeeName,
		"year": currentYear,
		"leave_balances": balances,
	})
}

type UpdateLeaveBalanceDTO struct {
	LeaveTypeID        string `json:"leave_type_id" binding:"required"`
	AllocatedDays      *int   `json:"allocated_days"`
	UsedDays           *int   `json:"used_days"`
	CarriedForwardDays *int   `json:"carried_forward_days"`
	Year               *int   `json:"year"`
}

// PUT /employees/:id/leave-balances
func (h *EmployeeHandler) UpdateLeaveBalances(c *gin.Context) {
	employeeID := c.Param("id")
	
	// Validate employee exists
	var employeeName string
	if err := h.Pool.QueryRow(context.Background(), "SELECT name FROM employees WHERE id=$1", employeeID).Scan(&employeeName); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "employee not found"})
		return
	}

	var input UpdateLeaveBalanceDTO
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input", "details": err.Error()})
		return
	}

	// Validate leave type exists
	var leaveTypeName string
	if err := h.Pool.QueryRow(context.Background(), "SELECT name FROM leave_types WHERE id=$1", input.LeaveTypeID).Scan(&leaveTypeName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "leave_type_id not found"})
		return
	}

	// Set default year if not provided
	year := time.Now().Year()
	if input.Year != nil {
		year = *input.Year
	}

	// Validate year is reasonable
	if year < 2020 || year > 2050 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year must be between 2020 and 2050"})
		return
	}

	// Build dynamic update query
	query := `UPDATE employee_leave_balances SET `
	args := []interface{}{}
	argIdx := 1
	updates := []string{}

	if input.AllocatedDays != nil {
		if *input.AllocatedDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "allocated_days cannot be negative"})
			return
		}
		updates = append(updates, fmt.Sprintf("allocated_days=$%d", argIdx))
		args = append(args, *input.AllocatedDays)
		argIdx++
	}

	if input.UsedDays != nil {
		if *input.UsedDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "used_days cannot be negative"})
			return
		}
		updates = append(updates, fmt.Sprintf("used_days=$%d", argIdx))
		args = append(args, *input.UsedDays)
		argIdx++
	}

	if input.CarriedForwardDays != nil {
		if *input.CarriedForwardDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "carried_forward_days cannot be negative"})
			return
		}
		updates = append(updates, fmt.Sprintf("carried_forward_days=$%d", argIdx))
		args = append(args, *input.CarriedForwardDays)
		argIdx++
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one field must be provided for update"})
		return
	}

	query += strings.Join(updates, ", ")
	query += fmt.Sprintf(" WHERE employee_id=$%d AND leave_type_id=$%d AND year=$%d", argIdx, argIdx+1, argIdx+2)
	args = append(args, employeeID, input.LeaveTypeID, year)

	// Execute update
	result, err := h.Pool.Exec(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update leave balance", "details": err.Error()})
		return
	}

	if result.RowsAffected() == 0 {
		// Try to insert if record doesn't exist
		allocatedDays := 0
		usedDays := 0
		carriedForwardDays := 0
		
		if input.AllocatedDays != nil {
			allocatedDays = *input.AllocatedDays
		}
		if input.UsedDays != nil {
			usedDays = *input.UsedDays
		}
		if input.CarriedForwardDays != nil {
			carriedForwardDays = *input.CarriedForwardDays
		}
		
		_, err = h.Pool.Exec(context.Background(), `
			INSERT INTO employee_leave_balances (employee_id, leave_type_id, year, allocated_days, used_days, carried_forward_days)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, employeeID, input.LeaveTypeID, year, allocatedDays, usedDays, carriedForwardDays)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create leave balance", "details": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "leave balance updated successfully",
		"employee_id": employeeID,
		"employee_name": employeeName,
		"leave_type_id": input.LeaveTypeID,
		"leave_type_name": leaveTypeName,
		"year": year,
	})
}
