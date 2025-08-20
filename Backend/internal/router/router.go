package router

import (
	"leave-management/internal/handlers"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Setup(r *gin.Engine, pool *pgxpool.Pool) {
	eh := handlers.NewEmployeeHandler(pool)

	lh := handlers.NewLeaveTypeHandler(pool)

	lrh := handlers.NewLeaveRequestHandler(pool)

	r.POST("/leave-requests", lrh.ApplyLeave)
	r.GET("/leave-requests", lrh.ListLeaveRequests)
	r.GET("/leave-requests/:id", lrh.GetLeaveRequestByID)
	r.PUT("/leave-requests/:id/approve", lrh.ApproveLeaveRequest)
	r.PUT("/leave-requests/:id/reject", lrh.RejectLeaveRequest)
	r.PUT("/leave-requests/:id/cancel", lrh.CancelLeaveRequest)

	r.GET("/leave-types", lh.GetLeaveTypes)

	// health
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	// Employee endpoints
	r.POST("/employees", eh.CreateEmployee)
	r.GET("/employees", eh.ListEmployees)
	r.GET("/employees/:id", eh.GetEmployeeByID)
	r.PUT("/employees/:id", eh.UpdateEmployee)
	r.DELETE("/employees/:id", eh.DeactivateEmployee)
	r.GET("/employees/:id/leave-balances", eh.GetLeaveBalances)
	r.PUT("/employees/:id/leave-balances", eh.UpdateLeaveBalances)
}
