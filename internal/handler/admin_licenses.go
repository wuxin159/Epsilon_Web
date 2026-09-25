package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

type licenseUpsertReq struct {
	MachineCode string `json:"machine_code" binding:"required"`
	Days        int    `json:"days"`
	ExpireAt    int64  `json:"expire_at"`
	Note        string `json:"note"`
}

type licenseAdjustReq struct {
	MachineCode string `json:"machine_code" binding:"required"`
	DeltaDays   int    `json:"delta_days" binding:"required"`
}

type licenseView struct {
	MachineCode string `json:"machine_code"`
	ExpireAt    int64  `json:"expire_at"`
	Note        string `json:"note"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (d *adminDeps) registerLicenseRoutes(g *gin.RouterGroup) {
	// 列表
	g.GET("/api/licenses", func(c *gin.Context) {
		list, err := d.licenses.List()
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

	// 新增/覆盖 (原有语义: 从今天起 N 天)
	g.POST("/api/licenses", func(c *gin.Context) {
		var req licenseUpsertReq
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "days or expire_at required"})
			return
		}

		old, findErr := d.licenses.FindByMachineCode(req.MachineCode)
		if err := d.licenses.Upsert(req.MachineCode, expireAt, req.Note); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		action := "create"
		details := map[string]any{"expire_at": expireAt, "note": req.Note}
		if findErr == nil && old != nil {
			action = "replace"
			details["old_expire_at"] = old.ExpireAt
			details["old_note"] = old.Note
		}
		b, _ := json.Marshal(details)
		_ = d.audit.Log(actorOf(c), storage.AuditCatLicense, action, req.MachineCode, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true, "expire_at": expireAt})
	})

	// 调整时长 (+/- N 天, 从当前 expire_at 或 now 中较大的时间开始算)
	g.POST("/api/licenses/adjust", func(c *gin.Context) {
		var req licenseAdjustReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		old, err := d.licenses.FindByMachineCode(req.MachineCode)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "license not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		base := old.ExpireAt
		now := time.Now().Unix()
		if base < now {
			base = now // 已过期的按今天算基准, 避免减法后出现遥远未来
		}
		newExpire := time.Unix(base, 0).AddDate(0, 0, req.DeltaDays).Unix()

		if err := d.licenses.Upsert(req.MachineCode, newExpire, old.Note); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		action := "extend"
		if req.DeltaDays < 0 {
			action = "reduce"
		}
		b, _ := json.Marshal(map[string]any{
			"delta_days":    req.DeltaDays,
			"old_expire_at": old.ExpireAt,
			"new_expire_at": newExpire,
		})
		_ = d.audit.Log(actorOf(c), storage.AuditCatLicense, action, req.MachineCode, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true, "expire_at": newExpire})
	})

	// 删除
	g.DELETE("/api/licenses/:machine_code", func(c *gin.Context) {
		code := c.Param("machine_code")
		old, _ := d.licenses.FindByMachineCode(code)
		if err := d.licenses.Delete(code); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		details := ""
		if old != nil {
			b, _ := json.Marshal(map[string]any{"expire_at": old.ExpireAt, "note": old.Note})
			details = string(b)
		}
		_ = d.audit.Log(actorOf(c), storage.AuditCatLicense, "delete", code, details)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}
