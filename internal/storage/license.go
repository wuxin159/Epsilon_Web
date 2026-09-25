package storage

import (
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("license not found")

type License struct {
	ID          int64
	MachineCode string
	ExpireAt    int64
	Note        string
	CreatedAt   int64
	UpdatedAt   int64
}

type LicenseRepo struct {
	db *sql.DB
}

func NewLicenseRepo(db *sql.DB) *LicenseRepo {
	return &LicenseRepo{db: db}
}

func (r *LicenseRepo) FindByMachineCode(machineCode string) (*License, error) {
	row := r.db.QueryRow(
		`SELECT id, machine_code, expire_at, note, created_at, updated_at FROM licenses WHERE machine_code = ?`,
		machineCode,
	)
	var l License
	err := row.Scan(&l.ID, &l.MachineCode, &l.ExpireAt, &l.Note, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &l, nil
}

func (r *LicenseRepo) Upsert(machineCode string, expireAt int64, note string) error {
	now := time.Now().Unix()
	_, err := r.db.Exec(`
		INSERT INTO licenses (machine_code, expire_at, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(machine_code) DO UPDATE SET
			expire_at  = excluded.expire_at,
			note       = excluded.note,
			updated_at = excluded.updated_at
	`, machineCode, expireAt, note, now, now)
	return err
}

func (r *LicenseRepo) Delete(machineCode string) error {
	_, err := r.db.Exec(`DELETE FROM licenses WHERE machine_code = ?`, machineCode)
	return err
}

func (r *LicenseRepo) List() ([]License, error) {
	rows, err := r.db.Query(
		`SELECT id, machine_code, expire_at, note, created_at, updated_at FROM licenses ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []License
	for rows.Next() {
		var l License
		if err := rows.Scan(&l.ID, &l.MachineCode, &l.ExpireAt, &l.Note, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
