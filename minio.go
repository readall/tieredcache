package multitiercache

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioTier implements Tier for object archive (batched objects).
type MinioTier struct {
	client *minio.Client
	bucket string
}

func NewMinioTier(endpoint, accessKey, secretKey, bucket string, secure bool) (Tier, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, err
	}
	return &MinioTier{client: client, bucket: bucket}, nil
}

func (m *MinioTier) Name() string { return "minio" }

func (m *MinioTier) PutBatch(ctx context.Context, items []TierItem) error {
	// Simple: one object per batch (production: use multipart or prefix grouping)
	buf := bytes.NewBuffer(nil)
	for _, item := range items {
		fmt.Fprintf(buf, "%s:%s\n", item.Key, item.Value)
	}
	objectName := fmt.Sprintf("archive/%d.bin", time.Now().UnixNano())
	_, err := m.client.PutObject(ctx, m.bucket, objectName, buf, int64(buf.Len()), minio.PutObjectOptions{})
	return err
}

// Get retrieves an object from MinIO by key (promotion support).
// The key is hex-encoded to form the object name.
func (m *MinioTier) Get(ctx context.Context, key []byte) ([]byte, error) {
	objectName := "archive/" + hex.EncodeToString(key)
	obj, err := m.client.GetObject(ctx, m.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		// Return nil, nil for not found (promotion will skip this tier)
		return nil, nil
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// Delete removes an object from MinIO.
func (m *MinioTier) Delete(ctx context.Context, key []byte) error {
	objectName := "archive/" + hex.EncodeToString(key)
	err := m.client.RemoveObject(ctx, m.bucket, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio delete failed: %w", err)
	}
	return nil
}

// ErrMinioNotFound is returned when object is not found in MinIO.
var ErrMinioNotFound = errors.New("object not found in MinIO")
