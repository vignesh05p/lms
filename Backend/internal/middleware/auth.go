package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"leave-management/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthMiddleware struct {
	pool *pgxpool.Pool
}

func NewAuthMiddleware(pool *pgxpool.Pool) *AuthMiddleware {
	return &AuthMiddleware{pool: pool}
}

// JWT secret key (in production, use environment variable)
var jwtSecret = []byte(os.Getenv("JWT_SECRET"))

// Authenticate middleware validates JWT token and sets user context
func (am *AuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Check if it's a Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// Parse and validate JWT token
		token, err := jwt.ParseWithClaims(tokenString, &models.JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			// Validate signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token", "details": err.Error()})
			c.Abort()
			return
		}

		if !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Extract claims
		claims, ok := token.Claims.(*models.JWTClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		// Check if token is expired
		if time.Now().Unix() > claims.Exp {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
			c.Abort()
			return
		}

		// Verify user still exists and is active
		var isActive bool
		err = am.pool.QueryRow(context.Background(), 
			"SELECT is_active FROM users WHERE id = $1 AND email = $2", 
			claims.UserID, claims.Email).Scan(&isActive)
		
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}

		if !isActive {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User account is deactivated"})
			c.Abort()
			return
		}

		// Set user context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Set("employee_id", claims.EmployeeID)

		c.Next()
	}
}

// RequireRole middleware checks if user has the required role
func (am *AuthMiddleware) RequireRole(requiredRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			c.Abort()
			return
		}

		role := userRole.(string)
		hasRole := false
		for _, requiredRole := range requiredRoles {
			if role == requiredRole {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePermission middleware checks if user has the required permission
func (am *AuthMiddleware) RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			c.Abort()
			return
		}

		role := userRole.(string)
		if !models.HasPermission(role, permission) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireOwnership middleware ensures user can only access their own data
func (am *AuthMiddleware) RequireOwnership(resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			c.Abort()
			return
		}

		userRole, _ := c.Get("role")
		role := userRole.(string)

		// Admin and HR can access all data
		if role == models.RoleAdmin || role == models.RoleHR {
			c.Next()
			return
		}

		// For managers, check if they're accessing their team's data
		if role == models.RoleManager {
			if am.canManagerAccessResource(c, userID.(string), resourceType) {
				c.Next()
				return
			}
		}

		// For employees, ensure they're accessing their own data
		if role == models.RoleEmployee {
			if am.canEmployeeAccessResource(c, userID.(string), resourceType) {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this resource"})
		c.Abort()
	}
}

// canManagerAccessResource checks if a manager can access a specific resource
func (am *AuthMiddleware) canManagerAccessResource(c *gin.Context, managerID, resourceType string) bool {
	switch resourceType {
	case "leave_request":
		requestID := c.Param("id")
		if requestID == "" {
			return false
		}

		// Check if the leave request belongs to a team member
		var employeeID string
		err := am.pool.QueryRow(context.Background(),
			`SELECT lr.employee_id FROM leave_requests lr 
			 JOIN employees e ON lr.employee_id = e.id 
			 WHERE lr.id = $1 AND e.manager_id = $2`,
			requestID, managerID).Scan(&employeeID)
		
		return err == nil

	case "employee":
		employeeID := c.Param("id")
		if employeeID == "" {
			return false
		}

		// Check if the employee reports to this manager
		var id string
		err := am.pool.QueryRow(context.Background(),
			"SELECT id FROM employees WHERE id = $1 AND manager_id = $2",
			employeeID, managerID).Scan(&id)
		
		return err == nil

	default:
		return false
	}
}

// canEmployeeAccessResource checks if an employee can access a specific resource
func (am *AuthMiddleware) canEmployeeAccessResource(c *gin.Context, employeeID, resourceType string) bool {
	switch resourceType {
	case "leave_request":
		requestID := c.Param("id")
		if requestID == "" {
			return false
		}

		// Check if the leave request belongs to this employee
		var id string
		err := am.pool.QueryRow(context.Background(),
			"SELECT id FROM leave_requests WHERE id = $1 AND employee_id = $2",
			requestID, employeeID).Scan(&id)
		
		return err == nil

	case "employee":
		paramEmployeeID := c.Param("id")
		// Employee can only access their own data
		return paramEmployeeID == employeeID

	case "leave_balance":
		paramEmployeeID := c.Param("id")
		// Employee can only access their own leave balances
		return paramEmployeeID == employeeID

	default:
		return false
	}
}

// OptionalAuth middleware allows optional authentication (for public endpoints)
func (am *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.Next()
			return
		}

		// Try to authenticate, but don't fail if it doesn't work
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenString, &models.JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err == nil && token.Valid {
			if claims, ok := token.Claims.(*models.JWTClaims); ok {
				c.Set("user_id", claims.UserID)
				c.Set("email", claims.Email)
				c.Set("role", claims.Role)
				c.Set("employee_id", claims.EmployeeID)
			}
		}

		c.Next()
	}
}
