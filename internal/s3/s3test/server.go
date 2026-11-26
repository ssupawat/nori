// Package s3test is an in-memory S3-compatible server for tests: path-style
// object PUT, GET, HEAD, DELETE, ListObjectsV2 and multipart uploads. It
// rejects unsigned requests and bodies whose x-amz-content-sha256 does not
// match.
package s3test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is an httptest server holding objects in memory.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	objects  map[string][]byte // "bucket/key"
	modified map[string]time.Time
	uploads  map[string]map[int][]byte
	nextID   int

	failPuts int // the next n object or part PUTs answer 500
	pageSize int // keys per list page (0 = 1000)
	requests map[string]int
}

// New starts a server; the caller closes it.
func New() *Server {
	s := &Server{
		objects:  map[string][]byte{},
		modified: map[string]time.Time{},
		uploads:  map[string]map[int][]byte{},
		requests: map[string]int{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Object returns a stored object's bytes.
func (s *Server) Object(bucket, key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[bucket+"/"+key]
	return b, ok
}

// Keys lists stored keys in a bucket, sorted.
func (s *Server) Keys(bucket string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.objects {
		if b, key, _ := strings.Cut(k, "/"); b == bucket {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// FailNextPuts makes the next n object or part PUTs answer 500.
func (s *Server) FailNextPuts(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failPuts = n
}

// SetPageSize caps the keys per list page (0 = 1000).
func (s *Server) SetPageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageSize = n
}

// Requests counts the requests received with method.
func (s *Server) Requests(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[method]
}

// OpenUploads counts multipart uploads neither completed nor aborted.
func (s *Server) OpenUploads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.uploads)
}

func xmlError(w http.ResponseWriter, status int, code, msg string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, msg)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[r.Method]++

	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		xmlError(w, http.StatusForbidden, "AccessDenied", "unsigned request")
		return
	}
	body, _ := io.ReadAll(r.Body)
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		xmlError(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch", "body hash mismatch")
		return
	}

	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := r.URL.Query()
	id := bucket + "/" + key

	switch {
	case r.Method == http.MethodGet && key == "" && q.Get("list-type") == "2":
		s.list(w, bucket, q)
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.nextID++
		uploadID := "upload-" + strconv.Itoa(s.nextID)
		s.uploads[uploadID] = map[int][]byte{}
		fmt.Fprintf(w, "<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>", uploadID)
	case r.Method == http.MethodPut && q.Has("uploadId"):
		parts, ok := s.uploads[q.Get("uploadId")]
		if !ok {
			xmlError(w, http.StatusNotFound, "NoSuchUpload", "unknown upload")
			return
		}
		if s.failPuts > 0 {
			s.failPuts--
			xmlError(w, http.StatusInternalServerError, "InternalError", "injected")
			return
		}
		n, _ := strconv.Atoi(q.Get("partNumber"))
		parts[n] = body
		w.Header().Set("ETag", fmt.Sprintf("\"etag-%d\"", n))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		parts, ok := s.uploads[q.Get("uploadId")]
		if !ok {
			xmlError(w, http.StatusNotFound, "NoSuchUpload", "unknown upload")
			return
		}
		var done struct {
			Parts []struct {
				PartNumber int    `xml:"PartNumber"`
				ETag       string `xml:"ETag"`
			} `xml:"Part"`
		}
		if err := xml.Unmarshal(body, &done); err != nil || len(done.Parts) != len(parts) {
			xmlError(w, http.StatusOK, "InvalidPart", "parts do not match")
			return
		}
		var all []byte
		for i, p := range done.Parts {
			if p.PartNumber != i+1 || p.ETag != fmt.Sprintf("\"etag-%d\"", p.PartNumber) {
				xmlError(w, http.StatusOK, "InvalidPart", "parts do not match")
				return
			}
			all = append(all, parts[p.PartNumber]...)
		}
		s.objects[id], s.modified[id] = all, time.Now()
		delete(s.uploads, q.Get("uploadId"))
		fmt.Fprint(w, "<CompleteMultipartUploadResult></CompleteMultipartUploadResult>")
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		delete(s.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		if s.failPuts > 0 {
			s.failPuts--
			xmlError(w, http.StatusInternalServerError, "InternalError", "injected")
			return
		}
		s.objects[id], s.modified[id] = body, time.Now()
	case r.Method == http.MethodHead, r.Method == http.MethodGet:
		b, ok := s.objects[id]
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
			} else {
				xmlError(w, http.StatusNotFound, "NoSuchKey", "no such key")
			}
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		if r.Method == http.MethodGet {
			w.Write(b)
		}
	case r.Method == http.MethodDelete:
		delete(s.objects, id)
		delete(s.modified, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		xmlError(w, http.StatusBadRequest, "NotImplemented", r.Method+" "+r.URL.String())
	}
}

func (s *Server) list(w http.ResponseWriter, bucket string, q map[string][]string) {
	prefix := ""
	if v := q["prefix"]; len(v) > 0 {
		prefix = v[0]
	}
	var keys []string
	for k := range s.objects {
		if b, key, _ := strings.Cut(k, "/"); b == bucket && strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	start := 0
	if v := q["continuation-token"]; len(v) > 0 {
		start, _ = strconv.Atoi(v[0])
	}
	size := s.pageSize
	if size <= 0 {
		size = 1000
	}
	end := min(start+size, len(keys))
	var b strings.Builder
	b.WriteString("<ListBucketResult>")
	for _, k := range keys[start:end] {
		fmt.Fprintf(&b, "<Contents><Key>%s</Key><Size>%d</Size><LastModified>%s</LastModified></Contents>",
			k, len(s.objects[bucket+"/"+k]), s.modified[bucket+"/"+k].UTC().Format(time.RFC3339))
	}
	if end < len(keys) {
		fmt.Fprintf(&b, "<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>", end)
	} else {
		b.WriteString("<IsTruncated>false</IsTruncated>")
	}
	b.WriteString("</ListBucketResult>")
	fmt.Fprint(w, b.String())
}
