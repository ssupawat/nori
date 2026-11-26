// Package s3 is a small client for S3-compatible object storage (AWS S3,
// Cloudflare R2, Backblaze B2, Tigris, MinIO): the handful of object
// operations nori's backups need, signed with AWS Signature Version 4 on
// net/http, without the AWS SDK.
package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyHash is the SHA-256 of an empty payload.
const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// unsignedHeaders are left out of the signature.
var unsignedHeaders = map[string]bool{
	"authorization":   true,
	"user-agent":      true,
	"content-length":  true,
	"accept-encoding": true,
	"expect":          true,
}

// Credentials sign requests for one access key.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// sign adds x-amz-date, x-amz-content-sha256 and Authorization to req.
// payloadHash is the hex SHA-256 of the body. Every header already on req
// is signed, plus host.
func sign(req *http.Request, creds Credentials, region, payloadHash string, now time.Time) {
	t := now.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if unsignedHeaders[lower] {
			continue
		}
		trimmed := make([]string, len(values))
		for i, v := range values {
			trimmed[i] = strings.Join(strings.Fields(v), " ")
		}
		headers[lower] = strings.Join(trimmed, ",")
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + headers[n] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := day + "/" + region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))

	key := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+creds.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// canonicalPath encodes each path segment once and keeps "//" and "." as
// they are.
func canonicalPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	segments := strings.Split(p, "/")
	for i, s := range segments {
		raw, err := url.PathUnescape(s)
		if err != nil {
			raw = s
		}
		segments[i] = uriEncode(raw)
	}
	return strings.Join(segments, "/")
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		values := append([]string(nil), q[k]...)
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// uriEncode percent-encodes everything except unreserved characters
// (A-Z a-z 0-9 - _ . ~), as SigV4 requires.
func uriEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

func hexSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
