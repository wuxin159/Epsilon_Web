package storage

import (
	"database/sql"
	"errors"
	"time"
)

var ErrFileNotFound = errors.New("file not found")

type FileMeta struct {
	ID         int64  `json:"id"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	MD5        string `json:"md5"`
	UploadedAt int64  `json:"uploaded_at"`
	UploadedBy string `json:"uploaded_by"`
	Note       string `json:"note"`
}

type FileRepo struct {
	db *sql.DB
}

func NewFileRepo(db *sql.DB) *FileRepo {
	return &FileRepo{db: db}
}

func (r *FileRepo) List() ([]FileMeta, error) {
	rows, err := r.db.Query(
		`SELECT id, filename, size, md5, uploaded_at, uploaded_by, note
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
		if err := rows.Scan(&f.ID, &f.Filename, &f.Size, &f.MD5, &f.UploadedAt, &f.UploadedBy, &f.Note); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *FileRepo) Get(filename string) (*FileMeta, error) {
	row := r.db.QueryRow(
		`SELECT id, filename, size, md5, uploaded_at, uploaded_by, note
		 FROM files WHERE filename = ?`,
		filename,
	)
	var f FileMeta
	err := row.Scan(&f.ID, &f.Filename, &f.Size, &f.MD5, &f.UploadedAt, &f.UploadedBy, &f.Note)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
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
	_, findErr := r.Get(f.Filename)
	isNew := errors.Is(findErr, ErrFileNotFound)

	_, err := r.db.Exec(`
		INSERT INTO files (filename, size, md5, uploaded_at, uploaded_by, note)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(filename) DO UPDATE SET
			size        = excluded.size,
			md5         = excluded.md5,
			uploaded_at = excluded.uploaded_at,
			uploaded_by = excluded.uploaded_by,
			note        = excluded.note
	`, f.Filename, f.Size, f.MD5, f.UploadedAt, f.UploadedBy, f.Note)
	return isNew, err
}

func (r *FileRepo) Delete(filename string) error {
	_, err := r.db.Exec(`DELETE FROM files WHERE filename = ?`, filename)
	return err
}
