package handler

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

const maxUploadBytes = 500 << 20 // 500 MB

func (d *adminDeps) registerFileRoutes(g *gin.RouterGroup) {
	dir := d.cfg.Download.RootDir
	_ = os.MkdirAll(dir, 0o755)

	// 列表
	g.GET("/api/files", func(c *gin.Context) {
		list, err := d.files.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if list == nil {
			list = []storage.FileMeta{}
		}
		c.JSON(http.StatusOK, gin.H{"files": list, "server_time": time.Now().Unix()})
	})

	// 上传 (multipart, 字段名 file, 可选 note)
	g.POST("/api/files/upload", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
		fh, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "field 'file' required: " + err.Error()})
			return
		}
		note := c.PostForm("note")

		cleanName := sanitizeFilename(fh.Filename)
		if cleanName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
			return
		}

		oldMeta, _ := d.files.Get(cleanName)

		src, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer src.Close()

		destPath := filepath.Join(dir, cleanName)
		tmpPath := destPath + ".uploading"

		dst, err := os.Create(tmpPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hasher := md5.New()
		n, err := io.Copy(io.MultiWriter(dst, hasher), src)
		dst.Close()
		if err != nil {
			os.Remove(tmpPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "write failed: " + err.Error()})
			return
		}
		if err := os.Rename(tmpPath, destPath); err != nil {
			os.Remove(tmpPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rename failed: " + err.Error()})
			return
		}
		md5sum := hex.EncodeToString(hasher.Sum(nil))

		meta := &storage.FileMeta{
			Filename:   cleanName,
			Size:       n,
			MD5:        md5sum,
			UploadedAt: time.Now().Unix(),
			UploadedBy: actorOf(c),
			Note:       note,
		}
		isNew, err := d.files.Upsert(meta)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db upsert: " + err.Error()})
			return
		}

		action := "upload"
		details := map[string]any{"size": n, "md5": md5sum, "note": note}
		if !isNew && oldMeta != nil {
			action = "replace"
			details["old_md5"] = oldMeta.MD5
			details["old_size"] = oldMeta.Size
		}
		b, _ := json.Marshal(details)
		_ = d.audit.Log(actorOf(c), storage.AuditCatFile, action, cleanName, string(b))

		c.JSON(http.StatusOK, gin.H{"ok": true, "file": meta})
	})

	// 下载 (管理员直接下载, 不走签名, 只走 BasicAuth)
	g.GET("/api/files/download/:name", func(c *gin.Context) {
		name := sanitizeFilename(c.Param("name"))
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad name"})
			return
		}
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found on disk"})
			return
		}
		c.FileAttachment(p, name)
	})

	// 删除
	g.DELETE("/api/files/:name", func(c *gin.Context) {
		name := sanitizeFilename(c.Param("name"))
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad name"})
			return
		}
		meta, err := d.files.Get(name)
		if err != nil {
			if errors.Is(err, storage.ErrFileNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		p := filepath.Join(dir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "remove file: " + err.Error()})
			return
		}
		if err := d.files.Delete(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db delete: " + err.Error()})
			return
		}
		b, _ := json.Marshal(map[string]any{"size": meta.Size, "md5": meta.MD5})
		_ = d.audit.Log(actorOf(c), storage.AuditCatFile, "delete", name, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}

// sanitizeFilename 阻止路径穿越, 只允许 basename。
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return ""
	}
	return name
}
