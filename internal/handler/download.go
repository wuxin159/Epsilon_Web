package handler

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/service"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

func RegisterDownload(r *gin.Engine, cfg *config.Config, repo *storage.LicenseRepo) {
	r.GET("/api/v1/download/:file", func(c *gin.Context) {
		machineCode := c.Query("machine_code")
		timestamp, _ := strconv.ParseInt(c.Query("timestamp"), 10, 64)
		sign := c.Query("sign")

		if err := service.Verify(cfg.Auth.Secret, machineCode, timestamp, sign, cfg.Auth.TimestampWindow); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}

		license, err := repo.FindByMachineCode(machineCode)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				c.JSON(http.StatusForbidden, gin.H{"error": "not authorized"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if license.ExpireAt < time.Now().Unix() {
			c.JSON(http.StatusForbidden, gin.H{"error": "expired"})
			return
		}

		filename := filepath.Base(c.Param("file"))
		if filename == "" || filename == "." || strings.ContainsAny(filename, `/\`) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad filename"})
			return
		}
		fullPath := filepath.Join(cfg.Download.RootDir, filename)
		c.FileAttachment(fullPath, filename)
	})
}
