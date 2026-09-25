package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// 由 main 包在启动时注入 (通过 ldflags 编译期填入 commit hash 和构建时间)。
var (
	BuildCommit = "dev"
	BuildTime   = "unknown"
	startTime   = time.Now().Unix()
)

// updateInFlight 防止并发触发更新。
var updateInFlight atomic.Bool

const defaultUpdateScript = "/opt/epsilon-src/scripts/server-update.sh"

func (d *adminDeps) registerServerRoutes(g *gin.RouterGroup) {
	// 当前进程版本信息
	g.GET("/api/server/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"commit":     BuildCommit,
			"built_at":   BuildTime,
			"started_at": startTime,
			"updating":   updateInFlight.Load(),
		})
	})

	// 触发 git pull + 重编 + 重启
	g.POST("/api/server/update", func(c *gin.Context) {
		if !updateInFlight.CompareAndSwap(false, true) {
			c.JSON(http.StatusConflict, gin.H{"error": "an update is already in progress"})
			return
		}
		// 兜底: 90 秒后自动解锁, 以防脚本失败没重启我们
		go func() {
			time.Sleep(90 * time.Second)
			updateInFlight.Store(false)
		}()

		scriptPath := os.Getenv("EPSILON_UPDATE_SCRIPT")
		if scriptPath == "" {
			scriptPath = defaultUpdateScript
		}
		if _, err := os.Stat(scriptPath); err != nil {
			updateInFlight.Store(false)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "update script not found at " + scriptPath,
			})
			return
		}

		// 用 systemd-run 起一个独立的 transient unit, 完全脱离本进程的 cgroup。
		// 这样即使 update 脚本内部调用 systemctl restart epsilon 杀掉本进程,
		// update 进程本身也不会被牵连。
		unitName := fmt.Sprintf("epsilon-update-%d", time.Now().Unix())
		cmd := exec.Command("systemd-run",
			"--no-block",
			"--collect",
			"--unit="+unitName,
			"--description=Epsilon self-update",
			"--",
			"bash", scriptPath,
		)
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf
		if err := cmd.Run(); err != nil {
			updateInFlight.Store(false)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "systemd-run failed: " + err.Error(),
				"stderr": errBuf.String(),
			})
			return
		}

		// 审计
		actor := actorOf(c)
		details, _ := json.Marshal(map[string]any{
			"commit_before": BuildCommit,
			"unit":          unitName,
		})
		_ = d.audit.Log(actor, "server", "update", "self", string(details))

		c.JSON(http.StatusAccepted, gin.H{
			"ok":            true,
			"commit_before": BuildCommit,
			"unit":          unitName,
			"message":       "update scheduled, service will restart shortly",
			"hint":          "查看日志: journalctl -u " + unitName + " -f",
		})
	})
}
