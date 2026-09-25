package handler

import (
	"errors"
	"net/http"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/service"
	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

const (
	CodeOK           = 0
	CodeUnauthorized = 1
	CodeExpired      = 2
	CodeInvalidSign  = 3
	CodeReplay       = 4
	CodeBadRequest   = 5
	CodeInternal     = 6
)

type authRequest struct {
	MachineCode string `json:"machine_code" binding:"required"`
	Timestamp   int64  `json:"timestamp"    binding:"required"`
	Sign        string `json:"sign"         binding:"required"`
}

type authResponse struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	ExpireAt   int64  `json:"expire_at,omitempty"`
	ServerTime int64  `json:"server_time"`
}

func RegisterAuth(r *gin.Engine, cfg *config.Config, repo *storage.LicenseRepo) {
	r.POST("/api/v1/auth/check", func(c *gin.Context) {
		var req authRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, authResponse{
				Code: CodeBadRequest, Message: "bad request",
				ServerTime: time.Now().Unix(),
			})
			return
		}

		if err := service.Verify(cfg.Auth.Secret, req.MachineCode, req.Timestamp, req.Sign, cfg.Auth.TimestampWindow); err != nil {
			code := CodeInvalidSign
			if errors.Is(err, service.ErrTimestampOutOfWindow) {
				code = CodeReplay
			}
			c.JSON(http.StatusOK, authResponse{
				Code: code, Message: err.Error(),
				ServerTime: time.Now().Unix(),
			})
			return
		}

		license, err := repo.FindByMachineCode(req.MachineCode)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				c.JSON(http.StatusOK, authResponse{
					Code: CodeUnauthorized, Message: "not authorized",
					ServerTime: time.Now().Unix(),
				})
				return
			}
			c.JSON(http.StatusOK, authResponse{
				Code: CodeInternal, Message: "internal error",
				ServerTime: time.Now().Unix(),
			})
			return
		}

		now := time.Now().Unix()
		if license.ExpireAt < now {
			c.JSON(http.StatusOK, authResponse{
				Code: CodeExpired, Message: "expired",
				ExpireAt: license.ExpireAt, ServerTime: now,
			})
			return
		}
		c.JSON(http.StatusOK, authResponse{
			Code: CodeOK, Message: "ok",
			ExpireAt: license.ExpireAt, ServerTime: now,
		})
	})
}
