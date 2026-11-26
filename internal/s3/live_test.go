package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// TestLiveEndpoint runs the client against real storage. It is skipped
// unless NORI_S3_TEST_ENDPOINT, NORI_S3_TEST_BUCKET,
// NORI_S3_TEST_ACCESS_KEY_ID and NORI_S3_TEST_SECRET_ACCESS_KEY are set
// (NORI_S3_TEST_REGION defaults to auto). The bucket must exist; the test
// writes and removes keys under nori-live-test/.
func TestLiveEndpoint(t *testing.T) {
	endpoint, bucket := os.Getenv("NORI_S3_TEST_ENDPOINT"), os.Getenv("NORI_S3_TEST_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("NORI_S3_TEST_ENDPOINT and NORI_S3_TEST_BUCKET are not set")
	}
	region := os.Getenv("NORI_S3_TEST_REGION")
	if region == "" {
		region = "auto"
	}
	c, err := New(endpoint, region, Credentials{
		AccessKeyID:     os.Getenv("NORI_S3_TEST_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("NORI_S3_TEST_SECRET_ACCESS_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.PartSize = 5 << 20 // the smallest part S3 accepts
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	small := []byte("nori live test")
	big := make([]byte, 11<<20) // three parts
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{"nori-live-test/small.json.gz": small, "nori-live-test/big dump.json.gz": big}
	t.Cleanup(func() {
		for key := range objects {
			_ = c.Delete(context.Background(), bucket, key)
		}
	})

	for key, data := range objects {
		// Act + Assert: upload, size check, download.
		if err := c.PutFile(ctx, bucket, key, writeFile(t, data)); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
		if size, err := c.Head(ctx, bucket, key); err != nil || size != int64(len(data)) {
			t.Fatalf("head %s: %d %v", key, size, err)
		}
		rc, err := c.Get(ctx, bucket, key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(got, data) {
			t.Fatalf("get %s: %d bytes differ", key, len(got))
		}
	}

	// Act + Assert: listing finds both, delete removes them.
	objs, err := c.List(ctx, bucket, "nori-live-test/")
	if err != nil || len(objs) != 2 {
		t.Fatalf("list: %v %v", objs, err)
	}
	for key := range objects {
		if err := c.Delete(ctx, bucket, key); err != nil {
			t.Fatalf("delete %s: %v", key, err)
		}
		if _, err := c.Head(ctx, bucket, key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("head after delete %s: %v", key, err)
		}
	}
}
