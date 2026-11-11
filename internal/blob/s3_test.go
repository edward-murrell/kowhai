package blob

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// The S3 wrapper against a fake S3 server. The compose test in CI runs
// the same calls against Garage.
func TestS3RoundTrip(t *testing.T) {
	backend := s3mem.New()
	srv := httptest.NewServer(gofakes3.New(backend).Server())
	defer srv.Close()
	if err := backend.CreateBucket("kowhai-attachments"); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Endpoint: srv.URL, Bucket: "kowhai-attachments", AccessKey: "GK1", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	data := []byte("hello kōwhai")
	if err := s.Put(ctx, "attachments/b/a", bytes.NewReader(data), int64(len(data)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := s.Get(ctx, "attachments/b/a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q", got)
	}
	if err := s.Delete(ctx, "attachments/b/a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, "attachments/b/a"); err == nil {
		t.Fatal("object still there")
	}
	if err := s.Delete(ctx, "attachments/b/missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}
