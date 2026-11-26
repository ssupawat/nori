package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"filippo.io/age"

	"nori/internal/s3"
)

// dumpName matches dump file names, plain or age-encrypted.
var dumpName = regexp.MustCompile(`^nori-dump-[0-9]{8}-[0-9.]+\.json\.gz(\.age)?$`)

// Remote ships dumps to a bucket on S3-compatible storage.
type Remote struct {
	Client     *s3.Client
	Bucket     string
	Prefix     string // no leading or trailing slash; "" = bucket root
	Keep       int    // remote dumps kept after an upload; 0 = never prune
	Passphrase string // non-empty: dumps are age-encrypted before upload
}

// ParseTarget splits "s3://bucket/prefix" into bucket and prefix.
func ParseTarget(target string) (bucket, prefix string, err error) {
	rest, ok := strings.CutPrefix(target, "s3://")
	if !ok {
		return "", "", fmt.Errorf("backup target %q: want s3://bucket/prefix", target)
	}
	bucket, prefix, _ = strings.Cut(rest, "/")
	if bucket == "" {
		return "", "", fmt.Errorf("backup target %q: no bucket", target)
	}
	return bucket, strings.Trim(prefix, "/"), nil
}

// Target is the s3:// form of the destination.
func (r *Remote) Target() string {
	if r.Prefix == "" {
		return "s3://" + r.Bucket
	}
	return "s3://" + r.Bucket + "/" + r.Prefix
}

func (r *Remote) key(name string) string {
	if r.Prefix == "" {
		return name
	}
	return r.Prefix + "/" + name
}

// Ship uploads the dump at path (encrypted first when a passphrase is
// set), checks the stored size, then prunes old remote dumps. Pruning
// happens only after a verified upload. It returns the object key.
func (r *Remote) Ship(ctx context.Context, path string) (string, error) {
	src, name := path, filepath.Base(path)
	if r.Passphrase != "" {
		src = path + ".age"
		name += ".age"
		if err := EncryptFile(path, src, r.Passphrase); err != nil {
			os.Remove(src)
			return "", err
		}
		defer os.Remove(src)
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	key := r.key(name)
	if err := r.Client.PutFile(ctx, r.Bucket, key, src); err != nil {
		return "", fmt.Errorf("upload %s: %w", key, err)
	}
	size, err := r.Client.Head(ctx, r.Bucket, key)
	if err != nil {
		return "", fmt.Errorf("check %s: %w", key, err)
	}
	if size != info.Size() {
		return "", fmt.Errorf("check %s: stored %d bytes, uploaded %d", key, size, info.Size())
	}
	if err := r.prune(ctx); err != nil {
		return key, fmt.Errorf("uploaded %s, but pruning old dumps failed: %w", key, err)
	}
	return key, nil
}

// List returns the remote dumps, newest first.
func (r *Remote) List(ctx context.Context) ([]s3.Object, error) {
	prefix := r.key("nori-dump-")
	objs, err := r.Client.List(ctx, r.Bucket, prefix)
	if err != nil {
		return nil, err
	}
	var out []s3.Object
	for _, o := range objs {
		if dumpName.MatchString(strings.TrimPrefix(o.Key, r.key(""))) {
			out = append(out, o)
		}
	}
	// timestamps in the names sort lexicographically
	sort.Slice(out, func(i, j int) bool { return out[i].Key > out[j].Key })
	return out, nil
}

func (r *Remote) prune(ctx context.Context) error {
	if r.Keep <= 0 {
		return nil
	}
	objs, err := r.List(ctx)
	if err != nil {
		return err
	}
	if len(objs) <= r.Keep {
		return nil
	}
	for _, o := range objs[r.Keep:] {
		if err := r.Client.Delete(ctx, r.Bucket, o.Key); err != nil {
			return err
		}
	}
	return nil
}

// Fetch downloads a remote dump into dir and returns the path of a plain
// .json.gz, decrypting an .age dump with the passphrase. name is a dump
// file name or "latest".
func (r *Remote) Fetch(ctx context.Context, name, dir string) (string, error) {
	if name == "latest" {
		objs, err := r.List(ctx)
		if err != nil {
			return "", err
		}
		if len(objs) == 0 {
			return "", fmt.Errorf("no dumps under %s", r.Target())
		}
		name = strings.TrimPrefix(objs[0].Key, r.key(""))
	}
	if !dumpName.MatchString(name) {
		return "", fmt.Errorf("%q is not a dump name (nori-dump-*.json.gz[.age])", name)
	}
	body, err := r.Client.Get(ctx, r.Bucket, r.key(name))
	if err != nil {
		return "", err
	}
	defer body.Close()
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if !strings.HasSuffix(path, ".age") {
		return path, nil
	}
	plain := strings.TrimSuffix(path, ".age")
	if err := DecryptFile(path, plain, r.Passphrase); err != nil {
		return "", err
	}
	os.Remove(path)
	return plain, nil
}

// EncryptFile writes src encrypted for passphrase (age, scrypt) to dst.
func EncryptFile(src, dst, passphrase string) error {
	rec, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(out, rec)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		out.Close()
		return err
	}
	if err := w.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// DecryptFile writes the age-encrypted src, decrypted with passphrase, to dst.
func DecryptFile(src, dst, passphrase string) error {
	if passphrase == "" {
		return errors.New("the dump is encrypted: set NORI_BACKUP_PASSPHRASE")
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	r, err := age.Decrypt(in, id)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w (wrong passphrase?)", filepath.Base(src), err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("decrypt %s: %w", filepath.Base(src), err)
	}
	return out.Close()
}
