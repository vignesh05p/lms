package models

import (
	"time"
)

// User represents an authenticated user in the system
type User struct {
	ID           string    `json:"id" db:"id"`
	EmployeeID   string    `json:"employee_id" db:"employee_id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"` // Never expose in JSON
	Role         string    `json:"role" db:"role"`
	IsActive     bool      `json:"is_active" db:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at" db:"last_login_at"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// LoginRequest represents the login payload
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginResponse represents the login response with JWT token
type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
}

// RefreshTokenRequest represents the refresh token payload
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// ChangePasswordRequest represents the change password payload
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

// JWTClaims represents the JWT token claims
type JWTClaims struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	EmployeeID string `json:"employee_id"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
}

// User roles constants
const (
	RoleEmployee = "employee"
	RoleManager  = "manager"
	RoleHR       = "hr"
	RoleAdmin    = "admin"
)

// IsValidRole checks if the role is valid
func IsValidRole(role string) bool {
	switch role {
	case RoleEmployee, RoleManager, RoleHR, RoleAdmin:
		return true
	default:
		return false
	}
}

// HasPermission checks if a role has permission for a specific action
func HasPermission(role, action string) bool {
	switch role {
	case RoleAdmin:
		return true // Admin has all permissions
	case RoleHR:
		// HR can do everything except system-level operations
		return action != "system_config"
	case RoleManager:
		// Manager can manage their team
		switch action {
		case "view_own_requests", "create_own_requests", "cancel_own_requests",
			"view_team_requests", "approve_team_requests", "reject_team_requests",
			"view_team_employees", "view_own_balances":
			return true
		default:
			return false
		}
	case RoleEmployee:
		// Employee can only manage their own data
		switch action {
		case "view_own_requests", "create_own_requests", "cancel_own_requests",
			"view_own_balances":
			return true
		default:
			return false
		}
	default:
		return false
	}
}
