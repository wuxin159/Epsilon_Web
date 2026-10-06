package storage

import (
	"database/sql"
	"errors"
	"time"
)

var ErrFileNotFound = errors.New("file not found")

type FileMeta struct {
	ID            int64  `json:"id"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	MD5           string `json:"md5"`
	UploadedAt    int64  `json:"uploaded_at"`
	UploadedBy    string `json:"uploaded_by"`
	Note          string `json:"note"`
	StorageName   string `json:"-"`
	DownloadToken string `json:"-"`
}

// DiskName also supports files uploaded before versioned storage was introduced.
func (f *FileMeta) DiskName() string {
	if f.StorageName != "" {
		return f.StorageName
	}
	return f.Filename
}

type FileRepo struct {
	db *sql.DB
}

func NewFileRepo(db *sql.DB) *FileRepo {
	return &FileRepo{db: db}
}

func (r *FileRepo) List() ([]FileMeta, error) {
	rows, err := r.db.Query(
		`SELECT id, filename, size, md5, uploaded_at, uploaded_by, note, storage_name, download_token
		 FROM files
		 ORDER BY uploaded_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileMeta
	for rows.Next() {
		var f FileMeta
		if err := rows.Scan(&f.ID, &f.Filename, &f.Size, &f.MD5, &f.UploadedAt, &f.UploadedBy, &f.Note, &f.StorageName, &f.DownloadToken); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *FileRepo) Get(filename string) (*FileMeta, error) {
	row := r.db.QueryRow(
		`SELECT id, filename, size, md5, uploaded_at, uploaded_by, note, storage_name, download_token
		 FROM files WHERE filename = ?`,
		filename,
	)
	var f FileMeta
	err := row.Scan(&f.ID, &f.Filename, &f.Size, &f.MD5, &f.UploadedAt, &f.UploadedBy, &f.Note, &f.StorageName, &f.DownloadToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *FileRepo) GetByDownloadToken(token string) (*FileMeta, error) {
	var f FileMeta
	err := r.db.QueryRow(`SELECT id, filename, size, md5, uploaded_at, uploaded_by, note, storage_name, download_token FROM files WHERE download_token = ?`, token).
		Scan(&f.ID, &f.Filename, &f.Size, &f.MD5, &f.UploadedAt, &f.UploadedBy, &f.Note, &f.StorageName, &f.DownloadToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Upsert 插入或按 filename 覆盖。返回是否是"新增"(true=新增, false=覆盖)。
func (r *FileRepo) Upsert(f *FileMeta) (bool, error) {
	if f.UploadedAt == 0 {
		f.UploadedAt = time.Now().Unix()
	}
	// 先查是否存在, 便于告知调用者是新增还是覆盖
	old, findErr := r.Get(f.Filename)
	isNew := errors.Is(findErr, ErrFileNotFound)
	if findErr != nil && !isNew {
		return false, findErr
	}
	if f.DownloadToken == "" {
		if old != nil {
			f.DownloadToken = old.DownloadToken
		}
		if f.DownloadToken == "" {
			var err error
			f.DownloadToken, err = newDownloadToken()
			if err != nil {
				return false, err
			}
		}
	}

	_, err := r.db.Exec(`
		INSERT INTO files (filename, size, md5, uploaded_at, uploaded_by, note, storage_name, download_token)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(filename) DO UPDATE SET
			size        = excluded.size,
			md5         = excluded.md5,
			uploaded_at = excluded.uploaded_at,
			uploaded_by = excluded.uploaded_by,
			note        = excluded.note,
			storage_name = excluded.storage_name,
			download_token = excluded.download_token
	`, f.Filename, f.Size, f.MD5, f.UploadedAt, f.UploadedBy, f.Note, f.StorageName, f.DownloadToken)
	return isNew, err
}

func (r *FileRepo) Delete(filename string) error {
	_, err := r.db.Exec(`DELETE FROM files WHERE filename = ?`, filename)
	return err
}
