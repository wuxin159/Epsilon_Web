package handler

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"epsilon/internal/storage"

	"github.com/gin-gonic/gin"
)

const maxUploadBytes = 500 << 20 // 500 MiB file, plus a bounded multipart envelope

func (d *adminDeps) registerFileRoutes(g *gin.RouterGroup) {
	dir := d.cfg.Download.RootDir

	// 列表
	g.GET("/api/files", func(c *gin.Context) {
		fileMu.RLock()
		list, err := d.files.List()
		fileMu.RUnlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		views := make([]fileView, 0, len(list))
		for i := range list {
			views = append(views, viewFile(d.cfg, &list[i]))
		}
		c.JSON(http.StatusOK, gin.H{"files": views, "server_time": time.Now().Unix()})
	})

	// 上传 (multipart, 字段名 file, 可选 note)
	g.POST("/api/files/upload", func(c *gin.Context) {
		if c.Request.ContentLength > maxUploadBytes+(1<<20) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file must be at most 500 MiB"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+(1<<20))
		fh, err := c.FormFile("file")
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file must be at most 500 MiB"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "field 'file' required: " + err.Error()})
			return
		}
		if fh.Size > maxUploadBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file must be at most 500 MiB"})
			return
		}
		note := c.PostForm("note")

		cleanName := sanitizeFilename(fh.Filename)
		if cleanName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
			return
		}

		src, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer src.Close()

		dst, err := os.CreateTemp(dir, ".epsilon-file-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		tmpPath := dst.Name()
		published := false
		defer func() {
			if !published {
				os.Remove(tmpPath)
			}
		}()
		hasher := md5.New()
		n, err := io.Copy(io.MultiWriter(dst, hasher), src)
		if err == nil {
			err = dst.Sync()
		}
		closeErr := dst.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "write failed: " + err.Error()})
			return
		}
		md5sum := hex.EncodeToString(hasher.Sum(nil))

		meta := &storage.FileMeta{
			Filename:    cleanName,
			Size:        n,
			MD5:         md5sum,
			UploadedAt:  time.Now().Unix(),
			UploadedBy:  actorOf(c),
			Note:        note,
			StorageName: filepath.Base(tmpPath),
		}
		fileMu.Lock()
		defer fileMu.Unlock()
		oldMeta, findErr := d.files.Get(cleanName)
		if findErr != nil && !errors.Is(findErr, storage.ErrFileNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db lookup failed"})
			return
		}
		isNew, err := d.files.Upsert(meta)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db upsert: " + err.Error()})
			return
		}
		published = true
		if oldMeta != nil {
			removeStoredFile(dir, oldMeta)
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

		c.JSON(http.StatusOK, gin.H{"ok": true, "file": viewFile(d.cfg, meta)})
	})

	// 下载 (管理员直接下载, 不走签名, 只走 BasicAuth)
	g.GET("/api/files/download/:name", func(c *gin.Context) {
		name := sanitizeFilename(c.Param("name"))
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad name"})
			return
		}
		fileMu.RLock()
		meta, err := d.files.Get(name)
		var f *os.File
		var info os.FileInfo
		if err == nil {
			f, info, err = openStoredFile(dir, meta)
		}
		fileMu.RUnlock()
		if err != nil {
			downloadError(c, err)
			return
		}
		serveOpenedFile(c, meta, f, info)
	})

	// 删除
	g.DELETE("/api/files/:name", func(c *gin.Context) {
		fileMu.Lock()
		defer fileMu.Unlock()
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
		if err := d.files.Delete(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db delete: " + err.Error()})
			return
		}
		removeStoredFile(dir, meta)
		b, _ := json.Marshal(map[string]any{"size": meta.Size, "md5": meta.MD5})
		_ = d.audit.Log(actorOf(c), storage.AuditCatFile, "delete", name, string(b))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}

func removeStoredFile(dir string, meta *storage.FileMeta) {
	name := meta.DiskName()
	if name == "" || name != sanitizeFilename(name) {
		return
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		// An active download may keep the old file open on Windows. Its link is
		// already revoked/replaced; failed cleanup must not undo the DB commit.
		slog.Warn("remove old file version", "file", meta.Filename, "err", err)
	}
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
