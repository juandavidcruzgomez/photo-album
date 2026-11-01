// Package storage puts objects in an S3-compatible store (SeaweedFS).
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ObjectStore is what the indexer needs from storage.
type ObjectStore interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
}

// OriginalKey is where an original image is stored.
func OriginalKey(hash, ext string) string {
	return fmt.Sprintf("originals/%s/%s.%s", hash[:2], hash, ext)
}

// ThumbnailKey is where an image's JPEG thumbnail is stored.
func ThumbnailKey(hash string) string {
	return fmt.Sprintf("thumbnails/%s/%s.jpg", hash[:2], hash)
}

// S3Config locates the bucket. Empty credentials mean anonymous access.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// S3 stores objects in one bucket.
type S3 struct {
	client *s3.Client
	bucket string
}

// NewS3 creates the client and the bucket if it doesn't exist yet.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	opts := s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		// SeaweedFS doesn't need the default CRC checksums; only send them
		// when an operation requires it.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if cfg.AccessKey != "" {
		opts.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	} else {
		opts.Credentials = aws.AnonymousCredentials{}
	}
	s := &S3{client: s3.New(opts), bucket: cfg.Bucket}
	if err := s.ensureBucket(ctx); err != nil {
		return nil, fmt.Errorf("bucket %s at %s: %w", cfg.Bucket, cfg.Endpoint, err)
	}
	return s, nil
}

func (s *S3) ensureBucket(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket}); err == nil {
		return nil
	}
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if errors.As(err, &owned) || errors.As(err, &exists) {
		return nil
	}
	return err
}

// Put uploads body under key, overwriting any existing object.
func (s *S3) Put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           &key,
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   &contentType,
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}
	return nil
}
