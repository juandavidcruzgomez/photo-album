package health

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/storage"
)

func TestRunReportsEveryCheck(t *testing.T) {
	pass := Check{"good", func(context.Context) (string, error) { return "fine", nil }}
	fail := Check{"bad", func(context.Context) (string, error) { return "", errors.New("boom\n  bang") }}

	var out bytes.Buffer
	err := Run(context.Background(), &out, fail, pass)
	if err == nil || !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("err = %v", err)
	}
	for _, want := range []string{"FAIL", "bad", "boom bang", "ok", "good", "fine"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}

	if err := Run(context.Background(), &out, pass); err != nil {
		t.Errorf("all passing: %v", err)
	}
}

func TestRunSetsDeadline(t *testing.T) {
	c := Check{"deadline", func(ctx context.Context) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("check has no deadline")
		}
		return "", nil
	}}
	if err := Run(context.Background(), &bytes.Buffer{}, c); err != nil {
		t.Error(err)
	}
}

func run(c Check) (string, error) { return c.Run(context.Background()) }

func TestPhotos(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(Photos(dir)); err == nil || !strings.Contains(err.Error(), "mounted") {
		t.Errorf("empty dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(Photos(dir)); err != nil {
		t.Errorf("populated dir: %v", err)
	}
	if _, err := run(Photos(filepath.Join(dir, "nope"))); err == nil {
		t.Error("missing dir passed")
	}
	if _, err := run(Photos("")); err == nil {
		t.Error("unset root passed")
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	empty := filepath.Join(dir, "empty")
	os.WriteFile(good, []byte("x"), 0o644)
	os.WriteFile(empty, nil, 0o644)

	if _, err := run(Files("models", good)); err != nil {
		t.Error(err)
	}
	_, err := run(Files("models", good, empty, dir, filepath.Join(dir, "missing")))
	if err == nil {
		t.Fatal("bad files passed")
	}
	if strings.Contains(err.Error(), good) || !strings.Contains(err.Error(), empty) {
		t.Errorf("err = %v", err)
	}
}

func TestStorage(t *testing.T) {
	tests := []struct {
		status  int
		wantErr bool
		detail  string
	}{
		{http.StatusOK, false, "bucket photos"},
		{http.StatusNotFound, false, "not created yet"},
		{http.StatusForbidden, true, ""},
	}
	for _, tt := range tests {
		var method string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method = r.Method
			w.WriteHeader(tt.status)
		}))
		detail, err := run(Storage(storage.S3Config{
			Endpoint: srv.URL, Region: "us-east-1", Bucket: "photos", AccessKey: "k", SecretKey: "s",
		}))
		srv.Close()
		if (err != nil) != tt.wantErr || !strings.Contains(detail, tt.detail) {
			t.Errorf("status %d: detail %q, err %v", tt.status, detail, err)
		}
		if method != http.MethodHead {
			t.Errorf("status %d: the check sent %s, want only HEAD", tt.status, method)
		}
	}
}
