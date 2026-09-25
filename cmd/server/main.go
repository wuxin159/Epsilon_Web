package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/handler"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

// 编译时通过 ldflags 注入:
//   go build -ldflags="-X main.buildCommit=abc -X main.buildTime=2026-..."
var (
	buildCommit = "dev"
	buildTime   = "unknown"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	db, err := storage.Open(cfg.Database.Path)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		slog.Error("migrate db", "err", err)
		os.Exit(1)
	}
	licenseRepo := storage.NewLicenseRepo(db)
	fileRepo := storage.NewFileRepo(db)
	auditRepo := storage.NewAuditRepo(db)

	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), handler.Logger())
	r.MaxMultipartMemory = 8 << 20 // 8MB in memory, rest spills to temp file

	// 把 build 信息注入 handler 包 (给 admin 面板显示当前版本用)
	handler.BuildCommit = buildCommit
	handler.BuildTime = buildTime

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Unix(),
			"commit": buildCommit,
		})
	})
	handler.RegisterAuth(r, cfg, licenseRepo)
	handler.RegisterDownload(r, cfg, licenseRepo)
	handler.RegisterUpdates(r, cfg, licenseRepo, fileRepo)
	handler.RegisterAdmin(r, cfg, licenseRepo, fileRepo, auditRepo)

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server starting", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
