package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nori/internal/s3/s3test"
)

var exampleCreds = Credentials{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}

// The three header-signing examples from the S3 SigV4 documentation.
func TestSignMatchesAWSExamples(t *testing.T) {
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name, method, url, payloadHash, signature string
		header                                    map[string]string
	}{
		{
			name: "GET object", method: "GET", url: "https://examplebucket.s3.amazonaws.com/test.txt",
			payloadHash: emptyHash, header: map[string]string{"Range": "bytes=0-9"},
			signature: "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			name: "GET bucket listing", method: "GET", url: "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J",
			payloadHash: emptyHash,
			signature:   "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
		{
			name: "PUT object with $ in the key", method: "PUT", url: "https://examplebucket.s3.amazonaws.com/test$file.text",
			payloadHash: "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
			header:      map[string]string{"Date": "Fri, 24 May 2013 00:00:00 GMT", "X-Amz-Storage-Class": "REDUCED_REDUNDANCY"},
			signature:   "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}

			// Act
			sign(req, exampleCreds, "us-east-1", tc.payloadHash, at)

			// Assert
			auth := req.Header.Get("Authorization")
			if !strings.HasSuffix(auth, "Signature="+tc.signature) {
				t.Fatalf("Authorization = %s\nwant signature %s", auth, tc.signature)
			}
		})
	}
}

// Signatures computed by botocore 1.44 (S3SigV4Auth) for requests built
// the way the client builds them: path-style, a key with a space,
// multipart and listing query strings.
func TestSignMatchesBotocore(t *testing.T) {
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	creds := Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	r2 := "https://acct.r2.cloudflarestorage.com"
	cases := []struct {
		name, endpoint, region, method, bucket, key string
		query                                       url.Values
		body, wantURL, signature                    string
	}{
		{"dump upload", r2, "auto", "PUT", "bkt", "prod/nori-dump-20261126-120000.000000001.json.gz.age", nil, "hello",
			r2 + "/bkt/prod/nori-dump-20261126-120000.000000001.json.gz.age", "ef9b6f5fd147eb15bd24e5c97e1b43735a6d5dbfb611bc78f363988f92a5d15d"},
		{"key with a space", r2, "auto", "PUT", "bkt", "prod/x y.json.gz", nil, "",
			r2 + "/bkt/prod/x%20y.json.gz", "caf42206e27b640b3ff897103dea263738c34edcc29109e2707dc74db5659d71"},
		{"start multipart", r2, "auto", "POST", "bkt", "prod/k", url.Values{"uploads": {""}}, "",
			r2 + "/bkt/prod/k?uploads=", "4f438572451560d84b35a50af778960f6baf04eb5d9006bb77e28cd8809f56fe"},
		{"upload part", "https://s3.ap-southeast-1.amazonaws.com", "ap-southeast-1", "PUT", "bkt", "prod/k",
			url.Values{"partNumber": {"2"}, "uploadId": {"abc+def"}}, "part",
			"https://s3.ap-southeast-1.amazonaws.com/bkt/prod/k?partNumber=2&uploadId=abc%2Bdef", "d8302908c6a7c569b3e37a2a4dd06bae005132dc807bdc4b245c41d633936398"},
		{"list page", r2, "auto", "GET", "bkt", "",
			url.Values{"list-type": {"2"}, "prefix": {"prod/nori-dump-"}, "continuation-token": {"a/b="}}, "",
			r2 + "/bkt?continuation-token=a%2Fb%3D&list-type=2&prefix=prod%2Fnori-dump-", "993e8200fe78e7b009bc1aa84a3c457b43e3004cdbf5845db6706b89f5cf60d9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			c, err := New(tc.endpoint, tc.region, creds)
			if err != nil {
				t.Fatal(err)
			}
			u := c.objectURL(tc.bucket, tc.key, tc.query)
			if u.String() != tc.wantURL {
				t.Fatalf("url = %s\nwant  %s", u.String(), tc.wantURL)
			}
			req, err := http.NewRequest(tc.method, u.String(), nil)
			if err != nil {
				t.Fatal(err)
			}
			req.URL = u

			// Act
			sign(req, creds, tc.region, hexSHA256([]byte(tc.body)), at)

			// Assert
			if auth := req.Header.Get("Authorization"); !strings.HasSuffix(auth, "Signature="+tc.signature) {
				t.Fatalf("Authorization = %s\nwant signature %s", auth, tc.signature)
			}
		})
	}
}

func newTestClient(t *testing.T, srv *s3test.Server) *Client {
	t.Helper()
	c, err := New(srv.URL, "auto", Credentials{AccessKeyID: "id", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClientObjectRoundTrip(t *testing.T) {
	// Arrange
	srv := s3test.New()
	defer srv.Close()
	c := newTestClient(t, srv)
	ctx := context.Background()
	data := []byte("hello backup")

	// Act + Assert: put, head, get.
	if err := c.PutFile(ctx, "b", "prod/x y.json.gz", writeFile(t, data)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if size, err := c.Head(ctx, "b", "prod/x y.json.gz"); err != nil || size != int64(len(data)) {
		t.Fatalf("head: %d %v", size, err)
	}
	rc, err := c.Get(ctx, "b", "prod/x y.json.gz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("get = %q", got)
	}

	// Act + Assert: delete, then head and get say not found, and a second delete is fine.
	if err := c.Delete(ctx, "b", "prod/x y.json.gz"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Head(ctx, "b", "prod/x y.json.gz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("head after delete: %v", err)
	}
	if _, err := c.Get(ctx, "b", "prod/x y.json.gz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := c.Delete(ctx, "b", "prod/x y.json.gz"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestClientMultipart(t *testing.T) {
	// Arrange: parts of 10 bytes, a 25-byte file.
	srv := s3test.New()
	defer srv.Close()
	c := newTestClient(t, srv)
	c.PartSize = 10
	data := []byte("0123456789abcdefghijKLMNO")

	// Act
	err := c.PutFile(context.Background(), "b", "big", writeFile(t, data))

	// Assert: three parts joined in order, no upload left open.
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got, _ := srv.Object("b", "big"); !bytes.Equal(got, data) {
		t.Fatalf("object = %q", got)
	}
	if srv.OpenUploads() != 0 {
		t.Fatalf("open uploads: %d", srv.OpenUploads())
	}
}

func TestClientRetriesThenAbortsMultipart(t *testing.T) {
	// Arrange
	srv := s3test.New()
	defer srv.Close()
	c := newTestClient(t, srv)
	c.PartSize = 10
	data := []byte("0123456789abcdefghij")

	// Act + Assert: two failures are retried away.
	srv.FailNextPuts(2)
	if err := c.PutFile(context.Background(), "b", "k", writeFile(t, data)); err != nil {
		t.Fatalf("put with two failures: %v", err)
	}

	// Act + Assert: three failures in a row fail the upload and abort it.
	srv.FailNextPuts(3)
	err := c.PutFile(context.Background(), "b", "k2", writeFile(t, data))
	if err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Fatalf("put with three failures: %v", err)
	}
	if _, ok := srv.Object("b", "k2"); ok || srv.OpenUploads() != 0 {
		t.Fatalf("failed upload left an object or an open upload")
	}
}

func TestClientListPages(t *testing.T) {
	// Arrange: five objects, two per page, one outside the prefix.
	srv := s3test.New()
	defer srv.Close()
	srv.SetPageSize(2)
	c := newTestClient(t, srv)
	ctx := context.Background()
	for _, k := range []string{"p/a", "p/b", "p/c", "p/d", "p/e", "q/z"} {
		if err := c.PutFile(ctx, "b", k, writeFile(t, []byte(k))); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	objs, err := c.List(ctx, "b", "p/")

	// Assert
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var keys []string
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	if strings.Join(keys, ",") != "p/a,p/b,p/c,p/d,p/e" || objs[0].Size != 3 || objs[0].LastModified.IsZero() {
		t.Fatalf("list = %+v", objs)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	for _, tc := range []struct{ endpoint, region, id, secret string }{
		{"ftp://x", "auto", "a", "b"},
		{"https://x", "", "a", "b"},
		{"https://x", "auto", "", "b"},
		{"https://x", "auto", "a", ""},
	} {
		if _, err := New(tc.endpoint, tc.region, Credentials{tc.id, tc.secret}); err == nil {
			t.Errorf("New(%+v) accepted", tc)
		}
	}
}
