package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go/aws"

	"vozko/domain/media"
)

type S3Service struct {
	client *s3.Client
	bucket string
}

var _ media.FileStorage = (*S3Service)(nil)

func NewS3Service() *S3Service {
	accessKeyId := os.Getenv("CLOUDFLARE_R2_KEY_ID")
	accessKeySecret := os.Getenv("CLOUDFLARE_R2_SECRET_KEY")
	accountId := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	bucket := os.Getenv("CLOUDFLARE_R2_BUCKET_NAME")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyId, accessKeySecret, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		log.Fatal("Unable to load config.")
	} else {
		log.Println("Config loaded.")
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountId))
	})
	return &S3Service{
		client: s3Client,
		bucket: bucket,
	}
}

func resolveContentType(key string, data []byte, declared string) string {
	if ct := strings.TrimSpace(declared); ct != "" {
		return ct
	}
	if ext := path.Ext(key); ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}

func (s *S3Service) UploadFile(key string, data []byte, contentType string) error {
	resolved := resolveContentType(key, data, contentType)
	_, err := s.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: aws.String(resolved),
	})
	if err != nil {
		log.Printf("[s3] upload failed key=%s content_type=%s: %v", key, resolved, err)
	}

	return err
}

func (s *S3Service) DownloadFile(ctx context.Context, key string) ([]byte, string, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", err
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := ""
	if output.ContentType != nil {
		contentType = *output.ContentType
	}
	return data, contentType, nil
}

func (s *S3Service) GetFileURL(key string) string {
	return fmt.Sprintf("%s/%s", os.Getenv("CLOUDFLARE_R2_ENDPOINT"), key)
}
