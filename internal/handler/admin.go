package handler

import (
	_ "embed"
	"log/slog"
	"net/http"

	"epsilon/internal/config"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

//go:embed admin_page.html
var adminPageHTML []byte

// adminDeps 汇总 admin 路由需要的所有依赖, 减少函数签名传参。
type adminDeps struct {
	cfg      *config.Config
	licenses *storage.LicenseRepo
	files    *storage.FileRepo
	audit    *storage.AuditRepo
}

func RegisterAdmin(r *gin.Engine, cfg *config.Config,
	licenses *storage.LicenseRepo,
	files *storage.FileRepo,
	audit *storage.AuditRepo,
) {
	if cfg.Admin.Username == "" || cfg.Admin.Password == "" {
		slog.Warn("admin panel disabled: admin.username or admin.password is empty in config")
		return
	}
	d := &adminDeps{cfg: cfg, licenses: licenses, files: files, audit: audit}

	group := r.Group("/admin", gin.BasicAuth(gin.Accounts{
		cfg.Admin.Username: cfg.Admin.Password,
	}))

	// 页面入口
	group.GET("", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin/") })
	group.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", adminPageHTML)
	})

	// 子路由分模块注册
	d.registerLicenseRoutes(group)
	d.registerFileRoutes(group)
	d.registerAuditRoutes(group)
}

// actorOf 从 gin BasicAuth 中间件里取当前登录的用户名。
func actorOf(c *gin.Context) string {
	if v, ok := c.Get(gin.AuthUserKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
