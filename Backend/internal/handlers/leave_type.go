package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

type createLeaveTypeDTO struct {
	Name                string `json:"name" binding:"required"`
	Description         string `json:"description"`
	MaxDaysPerYear      int    `json:"max_days_per_year"`
	CarryForwardAllowed bool   `json:"carry_forward_allowed"`
	MaxCarryForwardDays int    `json:"max_carry_forward_days"`
	IsActive            *bool  `json:"is_active"`
}

// POST /leave-types
func (h *LeaveTypeHandler) CreateLeaveType(c *gin.Context) {
	var in createLeaveTypeDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input", "details": err.Error()})
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if in.MaxDaysPerYear < 0 || in.MaxCarryForwardDays < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days cannot be negative"})
		return
	}
	if !in.CarryForwardAllowed {
		in.MaxCarryForwardDays = 0
	}
	isActive := true
	if in.IsActive != nil {
		isActive = *in.IsActive
	}
	var id string
	if err := h.pool.QueryRow(
		context.Background(),
		`INSERT INTO leave_types (name, description, max_days_per_year, carry_forward_allowed, max_carry_forward_days, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		name, in.Description, in.MaxDaysPerYear, in.CarryForwardAllowed, in.MaxCarryForwardDays, isActive,
	).Scan(&id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create leave type failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":                   id,
		"name":                 name,
		"description":          in.Description,
		"max_days_per_year":    in.MaxDaysPerYear,
		"carry_forward_allowed": in.CarryForwardAllowed,
		"max_carry_forward_days": in.MaxCarryForwardDays,
		"is_active":            isActive,
	})
}

type updateLeaveTypeDTO struct {
	Name                *string `json:"name"`
	Description         *string `json:"description"`
	MaxDaysPerYear      *int    `json:"max_days_per_year"`
	CarryForwardAllowed *bool   `json:"carry_forward_allowed"`
	MaxCarryForwardDays *int    `json:"max_carry_forward_days"`
	IsActive            *bool   `json:"is_active"`
}

// PUT /leave-types/:id
func (h *LeaveTypeHandler) UpdateLeaveType(c *gin.Context) {
	id := c.Param("id")
	var in updateLeaveTypeDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input", "details": err.Error()})
		return
	}
	sets := []string{}
	args := []interface{}{}
	idx := 1
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name cannot be empty"})
			return
		}
		sets = append(sets, fmt.Sprintf("name=$%d", idx))
		args = append(args, name)
		idx++
	}
	if in.Description != nil {
		sets = append(sets, fmt.Sprintf("description=$%d", idx))
		args = append(args, *in.Description)
		idx++
	}
	if in.MaxDaysPerYear != nil {
		if *in.MaxDaysPerYear < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "max_days_per_year cannot be negative"})
			return
		}
		sets = append(sets, fmt.Sprintf("max_days_per_year=$%d", idx))
		args = append(args, *in.MaxDaysPerYear)
		idx++
	}
	if in.CarryForwardAllowed != nil {
		sets = append(sets, fmt.Sprintf("carry_forward_allowed=$%d", idx))
		args = append(args, *in.CarryForwardAllowed)
		idx++
	}
	if in.MaxCarryForwardDays != nil {
		if *in.MaxCarryForwardDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "max_carry_forward_days cannot be negative"})
			return
		}
		sets = append(sets, fmt.Sprintf("max_carry_forward_days=$%d", idx))
		args = append(args, *in.MaxCarryForwardDays)
		idx++
	}
	if in.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active=$%d", idx))
		args = append(args, *in.IsActive)
		idx++
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}
	query := "UPDATE leave_types SET " + strings.Join(sets, ", ") + ", updated_at=NOW() WHERE id=$" + fmt.Sprintf("%d", idx)
	args = append(args, id)
	ct, err := h.pool.Exec(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update leave type failed", "details": err.Error()})
		return
	}
	if ct.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "leave type not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "leave type updated"})
}

// DELETE /leave-types/:id (soft delete)
func (h *LeaveTypeHandler) DeleteLeaveType(c *gin.Context) {
	id := c.Param("id")
	ct, err := h.pool.Exec(context.Background(), `UPDATE leave_types SET is_active=false, updated_at=NOW() WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete leave type failed"})
		return
	}
	if ct.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "leave type not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "leave type deactivated"})
}
