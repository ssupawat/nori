package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nori/internal/backup"
	"nori/internal/s3"
)

// newRemote builds the remote backup destination from flags and the
// NORI_BACKUP_* environment variables.
func newRemote(target, endpoint, region string, keep int) (*backup.Remote, error) {
	bucket, prefix, err := backup.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		if region == "" || region == "auto" {
			return nil, errors.New("-backup-endpoint is empty: set it, or set -backup-region for AWS S3")
		}
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	client, err := s3.New(endpoint, region, s3.Credentials{
		AccessKeyID:     os.Getenv("NORI_BACKUP_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("NORI_BACKUP_SECRET_ACCESS_KEY"),
	})
	if err != nil {
		return nil, fmt.Errorf("%w (set NORI_BACKUP_ACCESS_KEY_ID and NORI_BACKUP_SECRET_ACCESS_KEY)", err)
	}
	return &backup.Remote{
		Client:     client,
		Bucket:     bucket,
		Prefix:     prefix,
		Keep:       keep,
		Passphrase: os.Getenv("NORI_BACKUP_PASSPHRASE"),
	}, nil
}

// remoteFlags adds -remote, -endpoint and -region to a subcommand,
// inheriting the global values.
func remoteFlags(fs *flag.FlagSet, target, endpoint, region string) (*string, *string, *string) {
	return fs.String("remote", target, "remote backups: s3://bucket/prefix"),
		fs.String("endpoint", endpoint, "S3 endpoint URL (empty = AWS S3 for -region)"),
		fs.String("region", region, "S3 signing region")
}

// runBackupsCmd implements `nori backups`: list the remote dumps, newest
// first.
func runBackupsCmd(args []string, target, endpoint, region string) int {
	fs := flag.NewFlagSet("backups", flag.ContinueOnError)
	remoteTarget, remoteEndpoint, remoteRegion := remoteFlags(fs, target, endpoint, region)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *remoteTarget == "" {
		fmt.Fprintln(os.Stderr, "usage: nori backups -remote s3://bucket/prefix [-endpoint URL] [-region R]")
		return 2
	}
	remote, err := newRemote(*remoteTarget, *remoteEndpoint, *remoteRegion, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	objs, err := remote.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list %s: %v\n", remote.Target(), err)
		return 1
	}
	if len(objs) == 0 {
		fmt.Printf("no dumps under %s\n", remote.Target())
		return 0
	}
	for _, o := range objs {
		fmt.Printf("%s  %10d  %s\n", o.LastModified.UTC().Format(time.RFC3339), o.Size, filepath.Base(o.Key))
	}
	return 0
}

// restoreSource returns a local plain dump for `nori restore` and where it
// came from. A dump that exists as a local file is used as is (decrypted
// when it ends in .age); "latest" or any other name is fetched from the
// remote. cleanup removes temporary files.
func restoreSource(dump, target, endpoint, region string) (path, source string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "nori-restore-")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	fail := func(err error) (string, string, func(), error) {
		cleanup()
		return "", "", nil, err
	}
	if _, statErr := os.Stat(dump); statErr == nil {
		if filepath.Ext(dump) != ".age" {
			return dump, dump, cleanup, nil
		}
		plain := filepath.Join(tmp, strings.TrimSuffix(filepath.Base(dump), ".age"))
		if err := backup.DecryptFile(dump, plain, os.Getenv("NORI_BACKUP_PASSPHRASE")); err != nil {
			return fail(err)
		}
		return plain, dump, cleanup, nil
	}
	if target == "" {
		return fail(fmt.Errorf("dump not found: %s", dump))
	}
	remote, err := newRemote(target, endpoint, region, 0)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	path, err = remote.Fetch(ctx, dump, tmp)
	if err != nil {
		return fail(err)
	}
	return path, remote.Target() + "/" + filepath.Base(path), cleanup, nil
}
