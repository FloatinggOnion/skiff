package s3

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLiveMinio(t *testing.T) {
	ep := os.Getenv("S3_PROBE_ENDPOINT")
	if ep == "" {
		t.Skip("no probe endpoint")
	}
	c := &Client{Endpoint: ep, Bucket: "offsite", AccessKey: "probeuser", SecretKey: "probepass123"}
	ctx := context.Background()
	if err := c.Check(ctx, "skiff"); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(ctx, "skiff/db 1/backup+x.sql", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	bad := *c
	bad.SecretKey = "wrong"
	if err := bad.Put(ctx, "x", strings.NewReader("a"), 1); err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := c.Delete(ctx, "skiff/db 1/backup+x.sql"); err != nil {
		t.Fatal(err)
	}
}
