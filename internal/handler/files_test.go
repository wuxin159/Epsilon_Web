package handler

import (
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/service"
	"epsilon/internal/storage"
	"github.com/gin-gonic/gin"
)

type fileTestServer struct {
	r    *gin.Engine
	db   *sql.DB
	cfg  *config.Config
	repo *storage.FileRepo
}

func newFileTestServer(t *testing.T) *fileTestServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Download.RootDir = filepath.Join(dir, "downloads")
	if err := os.MkdirAll(cfg.Download.RootDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Admin.Username, cfg.Admin.Password = "test-admin", "test-password"
	cfg.Auth.Secret, cfg.Auth.TimestampWindow = "test-only-secret", 300
	r := gin.New()
	repo := storage.NewFileRepo(db)
	licenses := storage.NewLicenseRepo(db)
	if err := licenses.Upsert("TEST-DEVICE", time.Now().Add(time.Hour).Unix(), ""); err != nil {
		t.Fatal(err)
	}
	RegisterAdmin(r, cfg, licenses, repo, storage.NewAuditRepo(db))
	RegisterFileLinks(r, cfg, repo)
	RegisterDownload(r, cfg, licenses, repo)
	return &fileTestServer{r: r, db: db, cfg: cfg, repo: repo}
}

func (s *fileTestServer) upload(name string, data []byte) (int, fileView, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", name)
	if err != nil {
		return 0, fileView{}, err
	}
	if _, err := f.Write(data); err != nil {
		return 0, fileView{}, err
	}
	if err := w.Close(); err != nil {
		return 0, fileView{}, err
	}
	req := httptest.NewRequest("POST", "/admin/api/files/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.SetBasicAuth(s.cfg.Admin.Username, s.cfg.Admin.Password)
	rr := httptest.NewRecorder()
	s.r.ServeHTTP(rr, req)
	var response struct {
		File fileView `json:"file"`
	}
	if rr.Code == http.StatusOK {
		err = json.Unmarshal(rr.Body.Bytes(), &response)
	}
	return rr.Code, response.File, err
}

func (s *fileTestServer) request(method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.r.ServeHTTP(rr, req)
	return rr
}

func TestFileLinkLifecycleAndRange(t *testing.T) {
	s := newFileTestServer(t)
	name := "客户端 + v1.apk"
	data := bytes.Repeat([]byte("0123456789"), 500000) // Above the previous proxy limit.
	status, view, err := s.upload(name, data)
	if err != nil || status != 200 {
		t.Fatalf("upload: %d %v", status, err)
	}
	if view.DownloadPath == "" {
		t.Fatal("missing download link")
	}
	rr := s.request("GET", view.DownloadPath, nil)
	if rr.Code != 200 || !bytes.Equal(rr.Body.Bytes(), data) {
		t.Fatalf("download: %d", rr.Code)
	}
	etag := rr.Header().Get("ETag")
	rr = s.request("GET", view.DownloadPath, map[string]string{"Range": "bytes=2-5"})
	if rr.Code != 206 || rr.Body.String() != "2345" {
		t.Fatalf("range: %d %q", rr.Code, rr.Body.String())
	}
	rr = s.request("HEAD", view.DownloadPath, nil)
	if rr.Code != 200 || rr.Body.Len() != 0 || rr.Header().Get("Content-Length") != fmt.Sprint(len(data)) {
		t.Fatal("HEAD failed")
	}
	if rr := s.request("GET", view.DownloadPath, map[string]string{"If-None-Match": etag}); rr.Code != 304 {
		t.Fatal("conditional download failed")
	}
	if rr := s.request("GET", strings.TrimSuffix(view.DownloadPath, url.PathEscape(name))+"wrong.apk", nil); rr.Code != 404 {
		t.Fatal("wrong filename accepted")
	}
	if rr := s.request("GET", "/files/"+strings.Repeat("0", 64)+"/file.apk", nil); rr.Code != 404 {
		t.Fatal("unknown link accepted")
	}
	if rr := s.request("GET", "/admin/api/files", nil); rr.Code != 401 {
		t.Fatal("admin list became public")
	}
	if rr := s.request("GET", "/api/v1/download/"+url.PathEscape(name), nil); rr.Code != 403 {
		t.Fatal("existing signature download lost verification")
	}

	status, replaced, err := s.upload(name, []byte("new version"))
	if status != 200 || err != nil || replaced.DownloadPath != view.DownloadPath {
		t.Fatal("replacement changed stable link")
	}
	rr = s.request("GET", view.DownloadPath, map[string]string{"Range": "bytes=2-5", "If-Range": etag})
	if rr.Code != 200 || rr.Body.String() != "new version" {
		t.Fatal("old validator returned stale range")
	}
	ts := time.Now().Unix()
	query := url.Values{"machine_code": {"TEST-DEVICE"}, "timestamp": {fmt.Sprint(ts)}, "sign": {service.Sign(s.cfg.Auth.Secret, "TEST-DEVICE", ts)}}
	if rr := s.request("GET", "/api/v1/download/"+url.PathEscape(name)+"?"+query.Encode(), nil); rr.Code != 200 || rr.Body.String() != "new version" {
		t.Fatal("signed download did not resolve new storage")
	}
	req := httptest.NewRequest("DELETE", "/admin/api/files/"+url.PathEscape(name), nil)
	req.SetBasicAuth(s.cfg.Admin.Username, s.cfg.Admin.Password)
	rr = httptest.NewRecorder()
	s.r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal("delete failed")
	}
	if rr := s.request("GET", view.DownloadPath, nil); rr.Code != 404 {
		t.Fatal("deleted link still valid")
	}
	status, again, err := s.upload(name, []byte("recreated"))
	if status != 200 || err != nil || again.DownloadPath == view.DownloadPath {
		t.Fatal("recreating a deleted file revived its old link")
	}
}

func TestConcurrentSameNameUploadsStayConsistent(t *testing.T) {
	s := newFileTestServer(t)
	const count = 12
	var wg sync.WaitGroup
	results := make(chan string, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, view, err := s.upload("client.bin", bytes.Repeat([]byte{byte(i)}, 32768))
			if err != nil || status != 200 {
				errs <- fmt.Errorf("upload %d: status=%d err=%v", i, status, err)
				return
			}
			results <- view.DownloadPath
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var path string
	for p := range results {
		if path != "" && path != p {
			t.Error("concurrent replacement changed token")
		}
		path = p
	}
	meta, err := s.repo.Get("client.bin")
	if err != nil {
		t.Fatal(err)
	}
	rr := s.request("GET", path, nil)
	digest := md5.Sum(rr.Body.Bytes())
	if rr.Code != 200 || int64(rr.Body.Len()) != meta.Size || hex.EncodeToString(digest[:]) != meta.MD5 {
		t.Fatal("content and metadata disagree")
	}
	entries, err := os.ReadDir(s.cfg.Download.RootDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("abandoned versions: entries=%d err=%v", len(entries), err)
	}
}

func TestFailedPublicationAndDeletionPreserveOldDownload(t *testing.T) {
	s := newFileTestServer(t)
	status, view, err := s.upload("client.bin", []byte("original"))
	if status != 200 || err != nil {
		t.Fatal("setup upload failed")
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_update BEFORE UPDATE ON files BEGIN SELECT RAISE(FAIL, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := s.upload("client.bin", []byte("replacement")); status != 500 {
		t.Fatal("failed DB write was accepted")
	}
	if rr := s.request("GET", view.DownloadPath, nil); rr.Code != 200 || rr.Body.String() != "original" {
		t.Fatal("failed upload damaged original")
	}
	entries, err := os.ReadDir(s.cfg.Download.RootDir)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed upload left a file")
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON files BEGIN SELECT RAISE(FAIL, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("DELETE", "/admin/api/files/client.bin", nil)
	req.SetBasicAuth(s.cfg.Admin.Username, s.cfg.Admin.Password)
	rr := httptest.NewRecorder()
	s.r.ServeHTTP(rr, req)
	if rr.Code != 500 {
		t.Fatal("failed DB deletion was accepted")
	}
	if rr := s.request("GET", view.DownloadPath, nil); rr.Code != 200 || rr.Body.String() != "original" {
		t.Fatal("failed delete damaged original")
	}
}

func TestOpenDownloadKeepsItsVersionDuringReplacement(t *testing.T) {
	s := newFileTestServer(t)
	status, view, err := s.upload("client.bin", []byte("original"))
	if status != 200 || err != nil {
		t.Fatal("setup upload failed")
	}
	meta, err := s.repo.Get("client.bin")
	if err != nil {
		t.Fatal(err)
	}
	opened, _, err := openStoredFile(s.cfg.Download.RootDir, meta)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	status, _, err = s.upload("client.bin", []byte("replacement"))
	if status != 200 || err != nil {
		t.Fatal("replacement failed")
	}
	old := make([]byte, len("original"))
	if _, err := opened.ReadAt(old, 0); err != nil || string(old) != "original" {
		t.Fatal("opened download changed version")
	}
	if rr := s.request("GET", view.DownloadPath, nil); rr.Code != 200 || rr.Body.String() != "replacement" {
		t.Fatal("new download did not get replacement")
	}
}

func TestOversizedUploadRejectedBeforeParsing(t *testing.T) {
	s := newFileTestServer(t)
	req := httptest.NewRequest("POST", "/admin/api/files/upload", strings.NewReader("unused"))
	req.ContentLength = maxUploadBytes + (1 << 20) + 1
	req.SetBasicAuth(s.cfg.Admin.Username, s.cfg.Admin.Password)
	rr := httptest.NewRecorder()
	s.r.ServeHTTP(rr, req)
	if rr.Code != 413 {
		t.Fatalf("oversize status=%d", rr.Code)
	}
}
