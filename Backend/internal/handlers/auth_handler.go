package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"time"

	"leave-management/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	pool *pgxpool.Pool
}

func NewAuthHandler(pool *pgxpool.Pool) *AuthHandler {
	return &AuthHandler{pool: pool}
}

// Register creates a new user account
// POST /auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var input struct {
		EmployeeID string `json:"employee_id" binding:"required"`
		Email      string `json:"email" binding:"required,email"`
		Password   string `json:"password" binding:"required,min=6"`
		Name       string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Check if employee exists
	var employeeID string
	err := h.pool.QueryRow(context.Background(),
		"SELECT id FROM employees WHERE employee_id = $1 AND email = $2",
		input.EmployeeID, input.Email).Scan(&employeeID)
	
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Employee not found or email mismatch"})
		return
	}

	// Check if user already exists
	var existingUser string
	err = h.pool.QueryRow(context.Background(),
		"SELECT id FROM users WHERE email = $1 OR employee_id = $2",
		input.Email, input.EmployeeID).Scan(&existingUser)
	
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Get employee role
	var role string
	err = h.pool.QueryRow(context.Background(),
		"SELECT role FROM employees WHERE id = $1", employeeID).Scan(&role)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get employee role"})
		return
	}

	// Create user
	var userID string
	err = h.pool.QueryRow(context.Background(),
		`INSERT INTO users (employee_id, email, password_hash, role, is_active, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, true, NOW(), NOW())
		 RETURNING id`,
		input.EmployeeID, input.Email, string(hashedPassword), role).Scan(&userID)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User registered successfully",
		"user_id": userID,
	})
}

// Login authenticates user and returns JWT token
// POST /auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var input models.LoginRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Get user by email
	var user models.User
	err := h.pool.QueryRow(context.Background(),
		`SELECT id, employee_id, email, password_hash, role, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE email = $1`,
		input.Email).Scan(
		&user.ID, &user.EmployeeID, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check if user is active
	if !user.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account is deactivated"})
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate JWT token
	token, err := h.generateJWTToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Generate refresh token
	refreshToken, err := h.generateRefreshToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	// Update last login time
	_, err = h.pool.Exec(context.Background(),
		"UPDATE users SET last_login_at = NOW() WHERE id = $1", user.ID)
	
	if err != nil {
		// Log error but don't fail the login
		fmt.Printf("Failed to update last login time: %v\n", err)
	}

	c.JSON(http.StatusOK, models.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         user,
	})
}

// RefreshToken generates a new JWT token using refresh token
// POST /auth/refresh
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var input models.RefreshTokenRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Validate refresh token
	var userID string
	var expiresAt time.Time
	err := h.pool.QueryRow(context.Background(),
		"SELECT user_id, expires_at FROM refresh_tokens WHERE token = $1 AND is_revoked = false",
		input.RefreshToken).Scan(&userID, &expiresAt)
	
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	// Check if refresh token is expired
	if time.Now().After(expiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
		return
	}

	// Get user details
	var user models.User
	err = h.pool.QueryRow(context.Background(),
		`SELECT id, employee_id, email, password_hash, role, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1`,
		userID).Scan(
		&user.ID, &user.EmployeeID, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	// Check if user is active
	if !user.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account is deactivated"})
		return
	}

	// Generate new JWT token
	token, err := h.generateJWTToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Generate new refresh token
	refreshToken, err := h.generateRefreshToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	// Revoke old refresh token
	_, err = h.pool.Exec(context.Background(),
		"UPDATE refresh_tokens SET is_revoked = true WHERE token = $1", input.RefreshToken)
	
	if err != nil {
		// Log error but don't fail the refresh
		fmt.Printf("Failed to revoke old refresh token: %v\n", err)
	}

	c.JSON(http.StatusOK, models.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         user,
	})
}

// ChangePassword allows users to change their password
// POST /auth/change-password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, _ := c.Get("user_id")
	
	var input models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Get current password hash
	var currentPasswordHash string
	err := h.pool.QueryRow(context.Background(),
		"SELECT password_hash FROM users WHERE id = $1", userID).Scan(&currentPasswordHash)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Verify current password
	err = bcrypt.CompareHashAndPassword([]byte(currentPasswordHash), []byte(input.CurrentPassword))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Current password is incorrect"})
		return
	}

	// Hash new password
	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Update password
	_, err = h.pool.Exec(context.Background(),
		"UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2",
		string(newPasswordHash), userID)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	// Revoke all refresh tokens for this user
	_, err = h.pool.Exec(context.Background(),
		"UPDATE refresh_tokens SET is_revoked = true WHERE user_id = $1", userID)
	
	if err != nil {
		// Log error but don't fail the password change
		fmt.Printf("Failed to revoke refresh tokens: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// Logout revokes the current refresh token
// POST /auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	userID, _ := c.Get("user_id")
	
	// Get refresh token from request body
	var input struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input", "details": err.Error()})
		return
	}

	// Revoke refresh token
	_, err := h.pool.Exec(context.Background(),
		"UPDATE refresh_tokens SET is_revoked = true WHERE token = $1 AND user_id = $2",
		input.RefreshToken, userID)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// GetProfile returns the current user's profile
// GET /auth/profile
func (h *AuthHandler) GetProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	
	var user models.User
	err := h.pool.QueryRow(context.Background(),
		`SELECT id, employee_id, email, password_hash, role, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1`,
		userID).Scan(
		&user.ID, &user.EmployeeID, &user.Email, &user.PasswordHash,
		&user.Role, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// generateJWTToken creates a new JWT token for the user
func (h *AuthHandler) generateJWTToken(user models.User) (string, error) {
	// Get JWT secret from environment
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "your-secret-key-change-in-production" // Default for development
	}

	// Create claims
	claims := models.JWTClaims{
		UserID:     user.ID,
		Email:      user.Email,
		Role:       user.Role,
		EmployeeID: user.EmployeeID,
		Exp:        time.Now().Add(24 * time.Hour).Unix(), // 24 hours
		Iat:        time.Now().Unix(),
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	
	// Sign token
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// generateRefreshToken creates a new refresh token for the user
func (h *AuthHandler) generateRefreshToken(userID string) (string, error) {
	// Generate random token
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	// Store refresh token in database
	_, err := h.pool.Exec(context.Background(),
		`INSERT INTO refresh_tokens (token, user_id, expires_at, is_revoked, created_at)
		 VALUES ($1, $2, $3, false, NOW())`,
		token, userID, time.Now().Add(7*24*time.Hour)) // 7 days
	
	if err != nil {
		return "", err
	}

	return token, nil
}
