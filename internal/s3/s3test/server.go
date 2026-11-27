// Package s3test serves an in-memory S3 (gofakes3) for tests, with failure
// injection and request counts.
package s3test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// Server is an httptest server backed by gofakes3.
type Server struct {
	*httptest.Server
	backend *s3mem.Backend

	mu       sync.Mutex
	failPuts int
	requests map[string]int
}

// New starts a server with the given buckets; the caller closes it.
func New(buckets ...string) *Server {
	s := &Server{backend: s3mem.New(), requests: map[string]int{}}
	for _, b := range buckets {
		if err := s.backend.CreateBucket(b); err != nil {
			panic(err)
		}
	}
	fake := gofakes3.New(s.backend).Server()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests[r.Method]++
		fail := r.Method == http.MethodPut && s.failPuts > 0
		if fail {
			s.failPuts--
		}
		s.mu.Unlock()
		if fail {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "<Error><Code>InternalError</Code><Message>injected</Message></Error>")
			return
		}
		fake.ServeHTTP(w, r)
	}))
	return s
}

// FailNextPuts makes the next n object or part PUTs answer 500.
func (s *Server) FailNextPuts(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failPuts = n
}

// Requests counts the requests received with method.
func (s *Server) Requests(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[method]
}

// Object returns a stored object's bytes.
func (s *Server) Object(bucket, key string) ([]byte, bool) {
	obj, err := s.backend.GetObject(bucket, key, nil)
	if err != nil {
		return nil, false
	}
	defer obj.Contents.Close()
	b, err := io.ReadAll(obj.Contents)
	return b, err == nil
}

// Keys lists stored keys in a bucket, sorted.
func (s *Server) Keys(bucket string) []string {
	list, err := s.backend.ListBucket(bucket, nil, gofakes3.ListBucketPage{})
	if err != nil {
		return nil
	}
	var keys []string
	for _, c := range list.Contents {
		keys = append(keys, c.Key)
	}
	sort.Strings(keys)
	return keys
}
