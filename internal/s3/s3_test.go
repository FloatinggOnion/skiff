package s3

import (
	"context"
	"io"
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
	rc, err := c.Get(ctx, "skiff/db 1/backup+x.sql")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	if err := c.Delete(ctx, "skiff/db 1/backup+x.sql"); err != nil {
		t.Fatal(err)
	}
}

func TestParseListing(t *testing.T) {
	l, err := parseListing(strings.NewReader(`<?xml version="1.0"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <IsTruncated>true</IsTruncated>
  <Contents><Key>docs/a b.txt</Key><LastModified>2026-11-02T10:00:00Z</LastModified><Size>12</Size></Contents>
  <CommonPrefixes><Prefix>docs/old/</Prefix></CommonPrefixes>
  <NextContinuationToken>tok</NextContinuationToken>
</ListBucketResult>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Objects) != 1 || l.Objects[0].Key != "docs/a b.txt" || l.Objects[0].Size != 12 || l.Objects[0].Modified.Unix() != 1793613600 {
		t.Fatalf("objects = %+v", l.Objects)
	}
	if len(l.Folders) != 1 || l.Folders[0] != "docs/old/" || l.NextToken != "tok" {
		t.Fatalf("listing = %+v", l)
	}
}

func TestEscapeQuery(t *testing.T) {
	if got := escapeQuery("a b/c+d~"); got != "a%20b%2Fc%2Bd~" {
		t.Fatalf("escapeQuery = %q", got)
	}
}
