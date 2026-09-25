package handler

import (
	_ "embed"
	"log/slog"
	"net/http"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

//go:embed admin_page.html
var adminPageHTML []byte

type upsertRequest struct {
	MachineCode string `json:"machine_code" binding:"required"`
	Days        int    `json:"days"`
	ExpireAt    int64  `json:"expire_at"`
	Note        string `json:"note"`
}

type licenseView struct {
	MachineCode string `json:"machine_code"`
	ExpireAt    int64  `json:"expire_at"`
	Note        string `json:"note"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func RegisterAdmin(r *gin.Engine, cfg *config.Config, repo *storage.LicenseRepo) {
	if cfg.Admin.Username == "" || cfg.Admin.Password == "" {
		slog.Warn("admin panel disabled: admin.username or admin.password is empty in config")
		return
	}

	group := r.Group("/admin", gin.BasicAuth(gin.Accounts{
		cfg.Admin.Username: cfg.Admin.Password,
	}))

	// 页面: GET /admin/  或  /admin
	group.GET("", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin/") })
	group.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", adminPageHTML)
	})

	// API: 列出所有授权
	group.GET("/api/licenses", func(c *gin.Context) {
		list, err := repo.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		views := make([]licenseView, 0, len(list))
		for _, l := range list {
			views = append(views, licenseView{
				MachineCode: l.MachineCode,
				ExpireAt:    l.ExpireAt,
				Note:        l.Note,
				CreatedAt:   l.CreatedAt,
				UpdatedAt:   l.UpdatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"licenses":    views,
			"server_time": time.Now().Unix(),
		})
	})

	// API: 新增/续期 (机器码存在则覆盖 expire_at 和 note)
	// 请求体二选一: 用 days (从今天开始 N 天) 或 用 expire_at (指定 unix 时间戳)
	group.POST("/api/licenses", func(c *gin.Context) {
		var req upsertRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var expireAt int64
		switch {
		case req.ExpireAt > 0:
			expireAt = req.ExpireAt
		case req.Days > 0:
			expireAt = time.Now().AddDate(0, 0, req.Days).Unix()
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "either days or expire_at is required"})
			return
		}
		if err := repo.Upsert(req.MachineCode, expireAt, req.Note); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "expire_at": expireAt})
	})

	// API: 删除
	group.DELETE("/api/licenses/:machine_code", func(c *gin.Context) {
		if err := repo.Delete(c.Param("machine_code")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}
