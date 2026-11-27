// Package s3 is the small slice of S3-compatible object storage (AWS S3,
// Cloudflare R2, Backblaze B2, Tigris, MinIO) that nori's backups use,
// on top of the AWS SDK for Go v2.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// DefaultPartSize is both the size above which uploads go multipart and
// the size of each part.
const DefaultPartSize = 64 << 20

// ErrNotFound is returned when the object does not exist.
var ErrNotFound = errors.New("s3: not found")

// Credentials sign requests for one access key.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// Client talks to one S3-compatible endpoint with path-style URLs
// (endpoint/bucket/key).
type Client struct {
	api      *awss3.Client
	PartSize int64 // 0 = DefaultPartSize; at least 5 MB
}

// Object is one entry of a listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// New builds a client for endpoint, e.g. https://<account>.r2.cloudflarestorage.com.
// Request checksums are sent only where S3 requires them.
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
	api := awss3.New(awss3.Options{
		Region:                     region,
		BaseEndpoint:               aws.String(u.String()),
		UsePathStyle:               true,
		Credentials:                credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		RetryMaxAttempts:           3,
	})
	return &Client{api: api}, nil
}

func (c *Client) partSize() int64 {
	if c.PartSize > 0 {
		return c.PartSize
	}
	return DefaultPartSize
}

func notFound(err error) bool {
	var re *smithyhttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == 404
}

// PutFile uploads the file at path to bucket/key: one PUT up to the part
// size, a multipart upload above it (aborted on failure).
func (c *Client) PutFile(ctx context.Context, bucket, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	uploader := manager.NewUploader(c.api, func(u *manager.Uploader) { u.PartSize = c.partSize() })
	_, err = uploader.Upload(ctx, &awss3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: f})
	return err
}

// Head returns the size of bucket/key, or ErrNotFound.
func (c *Client) Head(ctx context.Context, bucket, key string) (int64, error) {
	out, err := c.api.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if notFound(err) {
		return 0, fmt.Errorf("%w: %s/%s", ErrNotFound, bucket, key)
	}
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

// List returns every object under prefix, in key order.
func (c *Client) List(ctx context.Context, bucket, prefix string) ([]Object, error) {
	var out []Object
	pages := awss3.NewListObjectsV2Paginator(c.api, &awss3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, Object{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size), LastModified: aws.ToTime(o.LastModified)})
		}
	}
	return out, nil
}

// Delete removes bucket/key. A missing object is not an error.
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.api.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if notFound(err) {
		return nil
	}
	return err
}

// Get opens bucket/key for reading; the caller closes it.
func (c *Client) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	out, err := c.api.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if notFound(err) {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, bucket, key)
	}
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
