package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestKeys(t *testing.T) {
	if got := OriginalKey("abcdef", "jpg"); got != "originals/ab/abcdef.jpg" {
		t.Errorf("OriginalKey = %s", got)
	}
	if got := ThumbnailKey("abcdef"); got != "thumbnails/ab/abcdef.jpg" {
		t.Errorf("ThumbnailKey = %s", got)
	}
}

// TestS3 runs against a real endpoint, e.g. the local SeaweedFS:
// PHALBUM_TEST_S3_ENDPOINT=http://localhost:8333
func TestS3(t *testing.T) {
	endpoint := os.Getenv("PHALBUM_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("PHALBUM_TEST_S3_ENDPOINT not set")
	}
	ctx := context.Background()
	bucket := "phalbum-test"
	s, err := NewS3(ctx, S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket})
	if err != nil {
		t.Fatal(err)
	}
	// A second client must accept the existing bucket.
	if _, err := NewS3(ctx, S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket}); err != nil {
		t.Fatal(err)
	}
	key := "test/" + time.Now().Format("150405.000")
	if err := s.Put(ctx, key, "text/plain", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	obj, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(obj.Body)
	obj.Body.Close()
	if string(got) != "hello" || obj.Size != 5 || obj.ContentType != "text/plain" {
		t.Errorf("Get: %q size=%d type=%s", got, obj.Size, obj.ContentType)
	}
	if _, err := s.Get(ctx, "test/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing object: %v", err)
	}

	resp, err := http.Get(endpoint + "/" + bucket + "/" + key)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("got %q", body)
	}
}
