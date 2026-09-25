package storage

import (
	"database/sql"
	"strings"
	"time"
)

const (
	AuditCatLicense = "license"
	AuditCatFile    = "file"
)

type AuditEntry struct {
	ID       int64  `json:"id"`
	TS       int64  `json:"ts"`
	Actor    string `json:"actor"`
	Category string `json:"category"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	Details  string `json:"details"`
}

type AuditRepo struct {
	db *sql.DB
}

func NewAuditRepo(db *sql.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

// Log 追加一条审计记录。返回 error 只是为了透明, 调用方通常忽略。
func (r *AuditRepo) Log(actor, category, action, target, details string) error {
	_, err := r.db.Exec(
		`INSERT INTO audit_log (ts, actor, category, action, target, details)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), actor, category, action, target, details,
	)
	return err
}

type AuditQuery struct {
	Category string
	Target   string
	Action   string
	Since    int64
	Until    int64
	Limit    int
	Offset   int
}

func (r *AuditRepo) Query(q AuditQuery) ([]AuditEntry, error) {
	var (
		conds []string
		args  []interface{}
	)
	if q.Category != "" {
		conds = append(conds, "category = ?")
		args = append(args, q.Category)
	}
	if q.Target != "" {
		conds = append(conds, "target LIKE ?")
		args = append(args, "%"+q.Target+"%")
	}
	if q.Action != "" {
		conds = append(conds, "action = ?")
		args = append(args, q.Action)
	}
	if q.Since > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, q.Since)
	}
	if q.Until > 0 {
		conds = append(conds, "ts <= ?")
		args = append(args, q.Until)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	sqlStr := "SELECT id, ts, actor, category, action, target, details FROM audit_log" +
		where + " ORDER BY ts DESC LIMIT ? OFFSET ?"
	args = append(args, q.Limit, q.Offset)

	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Category, &e.Action, &e.Target, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
