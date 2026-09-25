package handler

import (
	"net/http"
	"strconv"

	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

func (d *adminDeps) registerAuditRoutes(g *gin.RouterGroup) {
	g.GET("/api/audit", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		offset, _ := strconv.Atoi(c.Query("offset"))
		entries, err := d.audit.Query(storage.AuditQuery{
			Category: c.Query("category"),
			Target:   c.Query("target"),
			Action:   c.Query("action"),
			Limit:    limit,
			Offset:   offset,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []storage.AuditEntry{}
		}
		c.JSON(http.StatusOK, gin.H{"entries": entries})
	})
}
