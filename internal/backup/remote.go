package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"filippo.io/age"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob" // file:// buckets
	_ "gocloud.dev/blob/s3blob"   // s3:// buckets
)

// dumpName matches dump file names, plain or age-encrypted.
var dumpName = regexp.MustCompile(`^nori-dump-[0-9]{8}-[0-9.]+\.json\.gz(\.age)?$`)

// Remote ships dumps to a bucket (s3:// or file://, see gocloud.dev/blob).
type Remote struct {
	Target     string // the bucket URL as configured
	Passphrase string // non-empty: dumps are age-encrypted before upload
	bucket     *blob.Bucket
}

// OpenRemote opens the bucket at target, e.g.
// s3://bucket?prefix=prod/&endpoint=https://<account>.r2.cloudflarestorage.com&region=auto&use_path_style=true.
// s3:// URLs send request checksums only where S3 requires them unless
// the URL says otherwise.
func OpenRemote(ctx context.Context, target, passphrase string) (*Remote, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "s3" && u.Scheme != "file") {
		return nil, fmt.Errorf("backup target %q: want an s3:// or file:// bucket URL", target)
	}
	if u.Scheme == "s3" {
		q := u.Query()
		for _, k := range []string{"request_checksum_calculation", "response_checksum_validation"} {
			if !q.Has(k) {
				q.Set(k, "when_required")
			}
		}
		u.RawQuery = q.Encode()
	}
	b, err := blob.OpenBucket(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("backup target %q: %w", target, err)
	}
	return &Remote{Target: target, Passphrase: passphrase, bucket: b}, nil
}

// Close releases the bucket.
func (r *Remote) Close() error { return r.bucket.Close() }

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Ship uploads the dump at path, age-encrypted when a passphrase is set,
// and checks the stored size. It returns the object name.
func (r *Remote) Ship(ctx context.Context, path string) (string, error) {
	name := filepath.Base(path)
	if r.Passphrase != "" {
		name += ".age"
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w, err := r.bucket.NewWriter(ctx, name, nil)
	if err != nil {
		return "", fmt.Errorf("upload %s: %w", name, err)
	}
	counted := &countingWriter{w: w}
	if err := r.encryptTo(counted, in); err != nil {
		cancel() // abandons the upload
		w.Close()
		return "", fmt.Errorf("upload %s: %w", name, err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("upload %s: %w", name, err)
	}
	attrs, err := r.bucket.Attributes(ctx, name)
	if err != nil {
		return "", fmt.Errorf("check %s: %w", name, err)
	}
	if attrs.Size != counted.n {
		return "", fmt.Errorf("check %s: stored %d bytes, uploaded %d", name, attrs.Size, counted.n)
	}
	return name, nil
}

func (r *Remote) encryptTo(dst io.Writer, src io.Reader) error {
	if r.Passphrase == "" {
		_, err := io.Copy(dst, src)
		return err
	}
	rec, err := age.NewScryptRecipient(r.Passphrase)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(dst, rec)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	return w.Close()
}

// List returns the dumps in the bucket, newest first.
func (r *Remote) List(ctx context.Context) ([]*blob.ListObject, error) {
	var out []*blob.ListObject
	it := r.bucket.List(&blob.ListOptions{Prefix: "nori-dump-"})
	for {
		obj, err := it.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if dumpName.MatchString(obj.Key) {
			out = append(out, obj)
		}
	}
	// timestamps in the names sort lexicographically
	sort.Slice(out, func(i, j int) bool { return out[i].Key > out[j].Key })
	return out, nil
}

// Fetch downloads a dump into dir as a plain .json.gz, decrypting an .age
// dump with the passphrase. name is a dump name or "latest". It returns
// the dump's name in the bucket and the local path.
func (r *Remote) Fetch(ctx context.Context, name, dir string) (string, string, error) {
	if name == "latest" {
		dumps, err := r.List(ctx)
		if err != nil {
			return "", "", err
		}
		if len(dumps) == 0 {
			return "", "", fmt.Errorf("no dumps in %s", r.Target)
		}
		name = dumps[0].Key
	}
	if !dumpName.MatchString(name) {
		return "", "", fmt.Errorf("%q is not a dump name (nori-dump-*.json.gz[.age])", name)
	}
	in, err := r.bucket.NewReader(ctx, name, nil)
	if err != nil {
		return "", "", err
	}
	defer in.Close()
	path := filepath.Join(dir, strings.TrimSuffix(name, ".age"))
	if err := writeDecrypted(path, in, strings.HasSuffix(name, ".age"), r.Passphrase); err != nil {
		return "", "", fmt.Errorf("%s: %w", name, err)
	}
	return name, path, nil
}

// DecryptFile writes the age-encrypted src, decrypted with passphrase, to dst.
func DecryptFile(src, dst, passphrase string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := writeDecrypted(dst, in, true, passphrase); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(src), err)
	}
	return nil
}

func writeDecrypted(path string, src io.Reader, encrypted bool, passphrase string) error {
	if encrypted {
		if passphrase == "" {
			return errors.New("the dump is encrypted: set NORI_BACKUP_PASSPHRASE")
		}
		id, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return err
		}
		if src, err = age.Decrypt(src, id); err != nil {
			return fmt.Errorf("decrypt: %w (wrong passphrase?)", err)
		}
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(path)
		return err
	}
	return out.Close()
}
