package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/service"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

type updateFile struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	MD5        string `json:"md5"`
	UploadedAt int64  `json:"uploaded_at"`
}

// RegisterUpdates 注册客户端拉取更新清单的公开接口。
// 客户端签名规则与 /auth/check 一致 (HMAC-SHA256 of "machine_code|timestamp")。
// 只有授权且未过期的机器码能拿到清单。
func RegisterUpdates(r *gin.Engine, cfg *config.Config, licenses *storage.LicenseRepo, files *storage.FileRepo) {
	r.GET("/api/v1/updates", func(c *gin.Context) {
		machineCode := c.Query("machine_code")
		timestamp, _ := strconv.ParseInt(c.Query("timestamp"), 10, 64)
		sign := c.Query("sign")

		if err := service.Verify(cfg.Auth.Secret, machineCode, timestamp, sign, cfg.Auth.TimestampWindow); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": CodeInvalidSign, "message": err.Error()})
			return
		}
		l, err := licenses.FindByMachineCode(machineCode)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				c.JSON(http.StatusOK, gin.H{"code": CodeUnauthorized, "message": "not authorized"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": CodeInternal, "message": "internal error"})
			return
		}
		if l.ExpireAt < time.Now().Unix() {
			c.JSON(http.StatusOK, gin.H{"code": CodeExpired, "message": "expired", "expire_at": l.ExpireAt})
			return
		}
		list, err := files.List()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": CodeInternal, "message": err.Error()})
			return
		}
		views := make([]updateFile, 0, len(list))
		for _, f := range list {
			views = append(views, updateFile{
				Name: f.Filename, Size: f.Size, MD5: f.MD5, UploadedAt: f.UploadedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code":        CodeOK,
			"files":       views,
			"expire_at":   l.ExpireAt,
			"server_time": time.Now().Unix(),
		})
	})
}
