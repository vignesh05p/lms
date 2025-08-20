package router

import (
	"leave-managemen/internal/handlers"
	"leave-management/internal/middleware"
	"leave-management/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Setup(r *gin.Engine, pool *pgxpool.Pool) {
	// Initialize handlers
	eh := handlers.NewEmployeeHandler(pool)
	lh := handlers.NewLeaveTypeHandler(pool)
	ah := handlers.NewAuditHandler(pool)
	lrh := handlers.NewLeaveRequestHandler(pool)
	authHandler := handlers.NewAuthHandler(pool)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(pool)

	// Public routes (no authentication required)
	public := r.Group("/")
	{
		public.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	}

	// Authentication routes
	auth := r.Group("/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		auth.POST("/refresh", authHandler.RefreshToken)
	}

	// Protected routes (authentication required)
	protected := r.Group("/")
	protected.Use(authMiddleware.Authenticate())
	{
		// Auth management (authenticated users only)
		authProtected := protected.Group("/auth")
		{
			authProtected.GET("/profile", authHandler.GetProfile)
			authProtected.POST("/change-password", authHandler.ChangePassword)
			authProtected.POST("/logout", authHandler.Logout)
		}

		// Leave Requests (role-based access)
		leaveRequests := protected.Group("/leave-requests")
		{
			// Employees can create their own requests
			leaveRequests.POST("", authMiddleware.RequirePermission("create_own_requests"), lrh.ApplyLeave)

			// Employees can view their own requests, managers can view team requests, HR/Admin can view all
			leaveRequests.GET("", authMiddleware.RequirePermission("view_own_requests"), lrh.ListLeaveRequests)

			// Employees can view their own request details
			leaveRequests.GET("/:id", authMiddleware.RequireOwnership("leave_request"), lrh.GetLeaveRequestByID)

			// Managers can approve/reject team requests, HR/Admin can approve/reject any
			leaveRequests.PUT("/:id/approve", authMiddleware.RequirePermission("approve_team_requests"), lrh.ApproveLeaveRequest)
			leaveRequests.PUT("/:id/reject", authMiddleware.RequirePermission("reject_team_requests"), lrh.RejectLeaveRequest)

			// Employees can cancel their own requests
			leaveRequests.PUT("/:id/cancel", authMiddleware.RequireOwnership("leave_request"), lrh.CancelLeaveRequest)
		}

		// Leave Types (HR/Admin only)
		leaveTypes := protected.Group("/leave-types")
		{
			leaveTypes.GET("", lh.GetLeaveTypes) // Anyone can view leave types
			leaveTypes.POST("", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), lh.CreateLeaveType)
			leaveTypes.PUT("/:id", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), lh.UpdateLeaveType)
			leaveTypes.DELETE("/:id", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), lh.DeleteLeaveType)
		}

		// Audit Logs (HR/Admin only)
		protected.GET("/audit-logs", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), ah.GetAuditLogs)

		// Employee Management (HR/Admin only)
		employees := protected.Group("/employees")
		{
			employees.POST("", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), eh.CreateEmployee)
			employees.GET("", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), eh.ListEmployees)
			employees.GET("/:id", authMiddleware.RequireOwnership("employee"), eh.GetEmployeeByID)
			employees.PUT("/:id", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), eh.UpdateEmployee)
			employees.DELETE("/:id", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), eh.DeactivateEmployee)

			// Leave Balances
			employees.GET("/:id/leave-balances", authMiddleware.RequireOwnership("leave_balance"), eh.GetLeaveBalances)
			employees.PUT("/:id/leave-balances", authMiddleware.RequireRole(models.RoleHR, models.RoleAdmin), eh.UpdateLeaveBalances)
		}
	}
}
