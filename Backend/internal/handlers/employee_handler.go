package handlers

import (
	"context"
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
