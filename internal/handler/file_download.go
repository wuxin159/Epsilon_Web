package handler

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"epsilon/internal/config"
	"epsilon/internal/storage"
	"github.com/gin-gonic/gin"
)

// Coordinate metadata lookup/file opening with publishing and removing versions.
// Downloads release this lock before streaming, so an active download keeps its
// opened version while another request publishes a replacement.
var fileMu sync.RWMutex

type fileView struct {
	*storage.FileMeta
	DownloadPath string `json:"download_path"`
	DownloadURL  string `json:"download_url,omitempty"`
}

func viewFile(cfg *config.Config, meta *storage.FileMeta) fileView {
	path := "/files/" + meta.DownloadToken + "/" + url.PathEscape(meta.Filename)
	v := fileView{FileMeta: meta, DownloadPath: path}
	if cfg.Download.BaseURL != "" {
		v.DownloadURL = strings.TrimRight(cfg.Download.BaseURL, "/") + path
	}
	return v
}

// RegisterFileLinks provides stable, unguessable links independent of machine licenses.
func RegisterFileLinks(r *gin.Engine, cfg *config.Config, files *storage.FileRepo) {
	h := func(c *gin.Context) {
		token := c.Param("token")
		if len(token) != 64 {
			c.Status(http.StatusNotFound)
			return
		}
		fileMu.RLock()
		meta, err := files.GetByDownloadToken(token)
		if err == nil && meta.Filename != c.Param("name") {
			err = storage.ErrFileNotFound
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
	}
	r.GET("/files/:token/:name", h)
	r.HEAD("/files/:token/:name", h)
}

func openStoredFile(dir string, meta *storage.FileMeta) (*os.File, os.FileInfo, error) {
	name := meta.DiskName()
	if name == "" || name != sanitizeFilename(name) {
		return nil, nil, storage.ErrFileNotFound
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, storage.ErrFileNotFound
	}
	return f, info, nil
}

func serveOpenedFile(c *gin.Context, meta *storage.FileMeta, f *os.File, info os.FileInfo) {
	defer f.Close()
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.Filename}))
	c.Header("Cache-Control", "private, no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	if meta.MD5 != "" {
		c.Header("ETag", strconv.Quote(meta.MD5))
	}
	http.ServeContent(c.Writer, c.Request, meta.Filename, info.ModTime(), f)
}

func downloadError(c *gin.Context, err error) {
	if errors.Is(err, storage.ErrFileNotFound) || errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to open file"})
}
