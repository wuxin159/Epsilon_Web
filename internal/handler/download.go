package handler

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/service"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

func RegisterDownload(r *gin.Engine, cfg *config.Config, repo *storage.LicenseRepo, files *storage.FileRepo) {
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

		filename := c.Param("file")
		if filename == "" || filename != sanitizeFilename(filename) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad filename"})
			return
		}
		fileMu.RLock()
		meta, err := files.Get(filename)
		// Preserve legacy downloads of manually placed files, but never expose
		// unpublished/versioned objects by their internal storage names.
		if errors.Is(err, storage.ErrFileNotFound) && !strings.HasPrefix(filename, ".epsilon-") {
			meta, err = &storage.FileMeta{Filename: filename}, nil
		}
		var f *os.File
		var info os.FileInfo
		if err == nil {
			f, info, err = openStoredFile(cfg.Download.RootDir, meta)
		}
		fileMu.RUnlock()
		if err != nil {
			downloadError(c, err)
			return
		}
		serveOpenedFile(c, meta, f, info)
	})
}
