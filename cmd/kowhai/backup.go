package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/edward-murrell/kowhai/internal/blob"
	"github.com/edward-murrell/kowhai/internal/config"
	"github.com/edward-murrell/kowhai/internal/store"
)

const keepSnapshots = 7

// backupJob makes a nightly VACUUM INTO snapshot. It is a fallback to
// Litestream, which replicates the database continuously. Snapshots are
// kept on disk and, when a backup bucket is set, uploaded to it.
func backupJob(log *slog.Logger, db *store.DB, cfg config.Config, bs *blob.S3) func(context.Context) error {
	dir := cfg.BackupDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(cfg.DBPath), "backups")
	}
	return func(ctx context.Context) error {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		name := "kowhai-" + time.Now().UTC().Format("20060102-150405") + ".db"
		path := filepath.Join(dir, name)
		if err := db.Snapshot(ctx, path); err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		log.Info("snapshot written", "path", path)
		if bs != nil && cfg.S3BackupBucket != "" {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			st, _ := f.Stat()
			key := "snapshots/" + name
			if _, err := bs.Client().PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(cfg.S3BackupBucket), Key: aws.String(key), Body: f, ContentLength: aws.Int64(st.Size()),
			}); err != nil {
				return fmt.Errorf("upload snapshot: %w", err)
			}
			log.Info("snapshot uploaded", "bucket", cfg.S3BackupBucket, "key", key)
		}
		old, _ := filepath.Glob(filepath.Join(dir, "kowhai-*.db"))
		sort.Strings(old)
		for len(old) > keepSnapshots {
			_ = os.Remove(old[0])
			old = old[1:]
		}
		return nil
	}
}
