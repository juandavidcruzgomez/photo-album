package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDSN(t *testing.T) {
	s := &Settings{PostgresUser: "u", PostgresPassword: "p@ss", PostgresDB: "photo_album", PostgresHost: "db", PostgresPort: "5432"}
	if got, want := s.DSN(), "postgres://u:p%40ss@db:5432/photo_album?sslmode=disable"; got != want {
		t.Errorf("DSN = %s, want %s", got, want)
	}
	s.DatabaseURL = "postgres://override"
	if got := s.DSN(); got != "postgres://override" {
		t.Errorf("DatabaseURL not used: %s", got)
	}
}

func TestLoadFromParentEnvFile(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "phalbum")
	os.Mkdir(module, 0o755)
	os.WriteFile(filepath.Join(root, ".env"), []byte("POSTGRES_USER=alice\nPOSTGRES_DB=photo_album\n"), 0o644)
	t.Chdir(module)
	for _, k := range []string{"DATABASE_URL", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_HOST", "POSTGRES_PORT"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.PostgresUser != "alice" || s.PostgresDB != "photo_album" || s.PostgresHost != "localhost" || s.PostgresPort != "5432" {
		t.Errorf("unexpected settings %+v", s)
	}
}

func TestLoadWithoutEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("POSTGRES_DB", "from_env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.PostgresDB != "from_env" {
		t.Errorf("PostgresDB = %s", s.PostgresDB)
	}
	if s.S3Bucket != "photos" || s.StoreOriginals {
		t.Errorf("unexpected S3 defaults %+v", s)
	}

	t.Setenv("STORE_ORIGINALS", "maybe")
	if _, err := Load(); err == nil {
		t.Error("invalid STORE_ORIGINALS should fail")
	}
}
