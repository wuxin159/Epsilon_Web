package storage

import (
	"path/filepath"
	"testing"
)

func TestFileMigrationPreservesLegacyFileAndLink(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE files (id INTEGER PRIMARY KEY, filename TEXT NOT NULL UNIQUE, size INTEGER NOT NULL, md5 TEXT NOT NULL, uploaded_at INTEGER NOT NULL, uploaded_by TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT ''); INSERT INTO files VALUES(1,'legacy.apk',10,'old-md5',1,'admin','original')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewFileRepo(db)
	old, err := repo.Get("legacy.apk")
	if err != nil {
		t.Fatal(err)
	}
	if old.DiskName() != "legacy.apk" || old.Note != "original" || len(old.DownloadToken) != 64 {
		t.Fatal("migration lost legacy data")
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	again, err := repo.GetByDownloadToken(old.DownloadToken)
	if err != nil || again.ID != old.ID {
		t.Fatal("repeat migration changed link")
	}
}
