// Package storage wraps the S3-compatible object store (MinIO for local dev, per
// agents_memory/MEMORY.md's decision to stay on MinIO for the whole hackathon rather than
// depending on a real Yandex Object Storage bucket).
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	mc     *minio.Client
	bucket string
}

type Config struct {
	Endpoint  string // e.g. "localhost:9000", no scheme
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}

	exists, err := mc.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("creating bucket %q: %w", cfg.Bucket, err)
		}
	}

	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// UploadDir uploads every regular file under localDir to the bucket, keyed by
// "<keyPrefix>/<path relative to localDir>".
func (c *Client) UploadDir(ctx context.Context, localDir, keyPrefix string) (int, error) {
	uploaded := 0
	err := filepath.WalkDir(localDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		key := keyPrefix + "/" + filepath.ToSlash(rel)

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening %s: %w", path, err)
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if _, err := c.mc.PutObject(ctx, c.bucket, key, f, info.Size(), minio.PutObjectOptions{}); err != nil {
			return fmt.Errorf("uploading %s to %s: %w", path, key, err)
		}
		uploaded++
		return nil
	})
	if err != nil {
		return uploaded, err
	}
	return uploaded, nil
}

// PutObject uploads a single in-memory blob (e.g. the git-history JSON export) at key.
func (c *Client) PutObject(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}
	return nil
}

// ListKeys lists every object key under prefix. Used by a consumer (e.g. Grade service) to
// discover what's available for a repo before fetching, without downloading anything yet.
func (c *Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("listing objects under %q: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// DeletePrefix removes every object under prefix (e.g. "repos/<uuid>/") and returns how many
// were removed. A missing prefix is not an error.
func (c *Client) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	objects := c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	toRemove := make(chan minio.ObjectInfo)
	var listErr error
	listed := 0
	go func() {
		defer close(toRemove)
		for obj := range objects {
			if obj.Err != nil {
				listErr = fmt.Errorf("listing objects under %q: %w", prefix, obj.Err)
				return
			}
			listed++
			toRemove <- obj
		}
	}()
	var removeErr error
	for rerr := range c.mc.RemoveObjects(ctx, c.bucket, toRemove, minio.RemoveObjectsOptions{}) {
		if removeErr == nil {
			removeErr = fmt.Errorf("removing %s: %w", rerr.ObjectName, rerr.Err)
		}
	}
	if listErr != nil {
		return 0, listErr
	}
	if removeErr != nil {
		return 0, removeErr
	}
	return listed, nil
}

// GetObject fetches a single object's contents by key.
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", key, err)
	}
	return data, nil
}

// BatchResult is one key's outcome from FetchBatch.
type BatchResult struct {
	Key  string
	Data []byte
	Err  error
}

// FetchBatch fetches multiple keys concurrently, bounded by concurrency (a sensible default is
// used if concurrency <= 0). Built for a consumer that needs to read many files from a repo's
// tree without loading the whole thing into memory at once or serializing every fetch — e.g.
// Grade service scanning a repo in batches to parallelize category scoring.
func (c *Client) FetchBatch(ctx context.Context, keys []string, concurrency int) []BatchResult {
	if concurrency <= 0 {
		concurrency = 8
	}
	results := make([]BatchResult, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, key := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := c.GetObject(ctx, key)
			results[i] = BatchResult{Key: key, Data: data, Err: err}
		}(i, key)
	}
	wg.Wait()
	return results
}
