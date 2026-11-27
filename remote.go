package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nori/internal/backup"
)

func openRemote(ctx context.Context, target string) (*backup.Remote, error) {
	return backup.OpenRemote(ctx, target, os.Getenv("NORI_BACKUP_PASSPHRASE"))
}

// runBackupsCmd implements `nori backups`: list the remote dumps, newest
// first.
func runBackupsCmd(args []string, inheritRemote string) int {
	fs := flag.NewFlagSet("backups", flag.ContinueOnError)
	target := fs.String("remote", inheritRemote, "bucket URL, as for -backup-remote")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *target == "" {
		fmt.Fprintln(os.Stderr, "usage: nori backups -remote <bucket URL>")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	remote, err := openRemote(ctx, *target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer remote.Close()
	dumps, err := remote.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list %s: %v\n", *target, err)
		return 1
	}
	if len(dumps) == 0 {
		fmt.Printf("no dumps in %s\n", *target)
	}
	for _, d := range dumps {
		fmt.Printf("%s  %10d  %s\n", d.ModTime.UTC().Format(time.RFC3339), d.Size, d.Name)
	}
	return 0
}

// restoreSource returns a local plain dump for `nori restore` and where it
// came from. A dump that exists as a local file is used as is (decrypted
// when it ends in .age); "latest" or any other name is fetched from the
// remote. cleanup removes temporary files.
func restoreSource(dump, target string) (path, source string, cleanup func(), err error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	remote, err := openRemote(ctx, target)
	if err != nil {
		return fail(err)
	}
	defer remote.Close()
	name, path, err := remote.Fetch(ctx, dump, tmp)
	if err != nil {
		return fail(err)
	}
	return path, name + " in " + target, cleanup, nil
}
