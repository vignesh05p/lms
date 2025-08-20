package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditHandler struct {
	pool *pgxpool.Pool
}

func NewAuditHandler(pool *pgxpool.Pool) *AuditHandler {
	return &AuditHandler{pool: pool}
}

// GET /audit-logs?table_name=&record_id=&action=&changed_by=&from=&to=&limit=
func (h *AuditHandler) GetAuditLogs(c *gin.Context) {
	q := `SELECT id, table_name, record_id, action, old_values, new_values, changed_by, changed_at
	      FROM audit_logs WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if v := c.Query("table_name"); v != "" {
		q += " AND table_name=$" + strconv.Itoa(idx)
		args = append(args, v)
		idx++
	}
	if v := c.Query("record_id"); v != "" {
		q += " AND record_id=$" + strconv.Itoa(idx)
		args = append(args, v)
		idx++
	}
	if v := c.Query("action"); v != "" {
		q += " AND action=$" + strconv.Itoa(idx)
		args = append(args, v)
		idx++
	}
	if v := c.Query("changed_by"); v != "" {
		q += " AND changed_by=$" + strconv.Itoa(idx)
		args = append(args, v)
		idx++
	}
	// time range filters (ISO8601 expected)
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q += " AND changed_at >= $" + strconv.Itoa(idx)
			args = append(args, t)
			idx++
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q += " AND changed_at <= $" + strconv.Itoa(idx)
			args = append(args, t)
			idx++
		}
	}

	q += " ORDER BY changed_at DESC"
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n > 0 && n <= 200 { limit = n }
		}
	}
	q += " LIMIT " + strconv.Itoa(limit)

	rows, err := h.pool.Query(context.Background(), q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		return
	}
	defer rows.Close()

	res := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id string
			tableName string
			recordID string
			action string
			oldValues map[string]interface{}
			newValues map[string]interface{}
			changedBy *string
			changedAt time.Time
		)
		if err := rows.Scan(&id, &tableName, &recordID, &action, &oldValues, &newValues, &changedBy, &changedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "row scan failed"})
			return
		}
		res = append(res, gin.H{
			"id": id,
			"table_name": tableName,
			"record_id": recordID,
			"action": action,
			"old_values": oldValues,
			"new_values": newValues,
			"changed_by": changedBy,
			"changed_at": changedAt,
		})
	}

	c.JSON(http.StatusOK, res)
}
