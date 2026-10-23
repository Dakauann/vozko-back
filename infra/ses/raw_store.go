package ses

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"vozko/domain/emailsending"
)

type ObjectStore interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type RawMessageStore struct {
	objects ObjectStore
	bucket  string
}

var _ emailsending.RawMessageStore = (*RawMessageStore)(nil)

func NewRawMessageStore(objects ObjectStore, bucket string) (*RawMessageStore, error) {
	if objects == nil || strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("%w: raw message store", ErrIncompleteConfig)
	}
	return &RawMessageStore{objects: objects, bucket: bucket}, nil
}

func (s *RawMessageStore) Fetch(ctx context.Context, objectKey string) ([]byte, error) {
	if !emailsending.ValidRawMessageKey(objectKey) {
		return nil, emailsending.ErrRawMessageKey
	}
	out, err := s.objects.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return nil, fmt.Errorf("ses: fetch raw message: %w", err)
	}
	defer out.Body.Close()
	if aws.ToInt64(out.ContentLength) > emailsending.MaxRawMessageBytes {
		return nil, emailsending.ErrRawMessageTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(out.Body, emailsending.MaxRawMessageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ses: read raw message: %w", err)
	}
	if len(data) > emailsending.MaxRawMessageBytes {
		return nil, emailsending.ErrRawMessageTooLarge
	}
	return data, nil
}

func (s *RawMessageStore) Delete(ctx context.Context, objectKey string) error {
	if !emailsending.ValidRawMessageKey(objectKey) {
		return emailsending.ErrRawMessageKey
	}
	if _, err := s.objects.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)}); err != nil {
		return fmt.Errorf("ses: delete raw message: %w", err)
	}
	return nil
}
