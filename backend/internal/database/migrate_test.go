package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigrationsOrdersAndChecksumsFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "0002_second.sql"), []byte("SELECT 2;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "0001_first.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "README.md"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := loadMigrations(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Version != 1 || items[1].Version != 2 {
		t.Fatalf("unexpected migration order: %+v", items)
	}
	if len(items[0].Checksum) != 64 || items[0].Checksum == items[1].Checksum {
		t.Fatal("unexpected migration checksums")
	}
}

func TestLoadMigrationsRejectsDuplicateVersion(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"0001_first.sql", "0001_again.sql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadMigrations(directory); err == nil {
		t.Fatal("duplicate version was accepted")
	}
}
