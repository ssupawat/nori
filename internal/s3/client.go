package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultPartSize is both the size above which uploads go multipart and
// the size of each part.
const DefaultPartSize = 64 << 20

// ErrNotFound is returned when the object does not exist.
var ErrNotFound = errors.New("s3: not found")

// Client talks to one S3-compatible endpoint with path-style URLs
// (endpoint/bucket/key).
type Client struct {
	Endpoint *url.URL
	Region   string
	Creds    Credentials
	HTTP     *http.Client
	PartSize int64            // 0 = DefaultPartSize
	Now      func() time.Time // nil = time.Now
}

// Object is one entry of a listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// New builds a client for endpoint, e.g. https://<account>.r2.cloudflarestorage.com.
func New(endpoint, region string, creds Credentials) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("s3: endpoint %q is not an http(s) URL", endpoint)
	}
	if region == "" {
		return nil, errors.New("s3: region is empty")
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return nil, errors.New("s3: access key id or secret is empty")
	}
	return &Client{Endpoint: u, Region: region, Creds: creds, HTTP: &http.Client{}}, nil
}

func (c *Client) partSize() int64 {
	if c.PartSize > 0 {
		return c.PartSize
	}
	return DefaultPartSize
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) objectURL(bucket, key string, query url.Values) *url.URL {
	u := *c.Endpoint
	segments := strings.Split(key, "/")
	encoded := make([]string, len(segments))
	for i, s := range segments {
		encoded[i] = uriEncode(s)
	}
	base := strings.TrimRight(u.Path, "/")
	u.Path = base + "/" + bucket
	u.RawPath = base + "/" + uriEncode(bucket)
	if key != "" {
		u.Path += "/" + key
		u.RawPath += "/" + strings.Join(encoded, "/")
	}
	u.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	return &u
}

// request sends one signed request, retrying network errors and 5xx
// responses up to three times. body may be nil. A non-2xx response is
// returned as an error with the service's code and message.
func (c *Client) request(ctx context.Context, method string, u *url.URL, body io.ReadSeeker, size int64, header http.Header) (*http.Response, error) {
	payloadHash := emptyHash
	if body != nil {
		h := sha256.New()
		if _, err := io.Copy(h, body); err != nil {
			return nil, err
		}
		payloadHash = hex.EncodeToString(h.Sum(nil))
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		var reader io.Reader
		if body != nil {
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
			reader = io.NopCloser(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
		if err != nil {
			return nil, err
		}
		req.URL = u
		if body != nil {
			req.ContentLength = size
		}
		for k, v := range header {
			req.Header[k] = v
		}
		sign(req, c.Creds, c.Region, payloadHash, c.now())
		res, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		if res.StatusCode >= 500 {
			lastErr = responseError(method, u, res)
			continue
		}
		if res.StatusCode >= 300 {
			return nil, responseError(method, u, res)
		}
		return res, nil
	}
	return nil, lastErr
}

func responseError(method string, u *url.URL, res *http.Response) error {
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s %s", ErrNotFound, method, u.Path)
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(raw, &e) == nil && e.Code != "" {
		return fmt.Errorf("s3: %s %s: %d %s: %s", method, u.Path, res.StatusCode, e.Code, e.Message)
	}
	return fmt.Errorf("s3: %s %s: %s", method, u.Path, res.Status)
}

// PutFile uploads the file at path to bucket/key: one PUT up to the part
// size, a multipart upload above it.
func (c *Client) PutFile(ctx context.Context, bucket, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= c.partSize() {
		res, err := c.request(ctx, http.MethodPut, c.objectURL(bucket, key, nil), io.NewSectionReader(f, 0, info.Size()), info.Size(), nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		return nil
	}
	return c.putMultipart(ctx, bucket, key, f, info.Size())
}

type completedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

func (c *Client) putMultipart(ctx context.Context, bucket, key string, f *os.File, size int64) error {
	res, err := c.request(ctx, http.MethodPost, c.objectURL(bucket, key, url.Values{"uploads": {""}}), nil, 0, nil)
	if err != nil {
		return err
	}
	var initiated struct {
		UploadID string `xml:"UploadId"`
	}
	err = xml.NewDecoder(res.Body).Decode(&initiated)
	res.Body.Close()
	if err != nil || initiated.UploadID == "" {
		return fmt.Errorf("s3: start multipart upload of %s: no upload id", key)
	}
	uploadID := initiated.UploadID
	abort := func() {
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if res, err := c.request(actx, http.MethodDelete, c.objectURL(bucket, key, url.Values{"uploadId": {uploadID}}), nil, 0, nil); err == nil {
			res.Body.Close()
		}
	}

	var parts []completedPart
	partSize := c.partSize()
	for n, off := 1, int64(0); off < size; n, off = n+1, off+partSize {
		length := min(partSize, size-off)
		q := url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {uploadID}}
		res, err := c.request(ctx, http.MethodPut, c.objectURL(bucket, key, q), io.NewSectionReader(f, off, length), length, nil)
		if err != nil {
			abort()
			return err
		}
		res.Body.Close()
		etag := res.Header.Get("ETag")
		if etag == "" {
			abort()
			return fmt.Errorf("s3: part %d of %s: no ETag", n, key)
		}
		parts = append(parts, completedPart{PartNumber: n, ETag: etag})
	}

	body, err := xml.Marshal(struct {
		XMLName xml.Name        `xml:"CompleteMultipartUpload"`
		Parts   []completedPart `xml:"Part"`
	}{Parts: parts})
	if err != nil {
		abort()
		return err
	}
	header := http.Header{"Content-Type": {"application/xml"}}
	res, err = c.request(ctx, http.MethodPost, c.objectURL(bucket, key, url.Values{"uploadId": {uploadID}}), bytes.NewReader(body), int64(len(body)), header)
	if err != nil {
		abort()
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		abort()
		return err
	}
	var e struct {
		XMLName xml.Name
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(raw, &e) == nil && e.XMLName.Local == "Error" {
		abort()
		return fmt.Errorf("s3: complete multipart upload of %s: %s: %s", key, e.Code, e.Message)
	}
	return nil
}

// Head returns the size of bucket/key, or ErrNotFound.
func (c *Client) Head(ctx context.Context, bucket, key string) (int64, error) {
	res, err := c.request(ctx, http.MethodHead, c.objectURL(bucket, key, nil), nil, 0, nil)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.ContentLength, nil
}

// List returns every object under prefix, in key order.
func (c *Client) List(ctx context.Context, bucket, prefix string) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		res, err := c.request(ctx, http.MethodGet, c.objectURL(bucket, "", q), nil, 0, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			IsTruncated           bool   `xml:"IsTruncated"`
			NextContinuationToken string `xml:"NextContinuationToken"`
			Contents              []struct {
				Key          string    `xml:"Key"`
				Size         int64     `xml:"Size"`
				LastModified time.Time `xml:"LastModified"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("s3: list %s/%s: %w", bucket, prefix, err)
		}
		for _, o := range page.Contents {
			out = append(out, Object{Key: o.Key, Size: o.Size, LastModified: o.LastModified})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}

// Delete removes bucket/key. A missing object is not an error.
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	res, err := c.request(ctx, http.MethodDelete, c.objectURL(bucket, key, nil), nil, 0, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// Get opens bucket/key for reading; the caller closes it.
func (c *Client) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	res, err := c.request(ctx, http.MethodGet, c.objectURL(bucket, key, nil), nil, 0, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
