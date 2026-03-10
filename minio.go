package multitiercache

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/minio/minio-go/v7"
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
