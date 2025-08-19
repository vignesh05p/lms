package models

import "time"

type CreateEmployeeDTO struct {
	Name         string    `json:"name" binding:"required"`
	Email        string    `json:"email" binding:"required"`
	DepartmentID string    `json:"department_id" binding:"required"` // UUID
	JoiningDate  time.Time `json:"joining_date" binding:"required"`  // ISO date
	EmployeeID   string    `json:"employee_id"`                      // optional (we can auto-generate)
}

type Employee struct {
	ID           string    `json:"id"`
	EmployeeID   string    `json:"employee_id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	DepartmentID string    `json:"department_id"`
	JoiningDate  time.Time `json:"joining_date"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}
