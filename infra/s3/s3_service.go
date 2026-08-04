package s3

import (
	"bytes"
	"context"
	"fmt"
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

// resolveContentType picks the Content-Type the CDN will serve.
//
// Order matters: what the caller declared beats the key's extension, which
// beats sniffing, because only the caller knows the difference between an
// audio/ogg voice note and an application/ogg container with the same bytes.
//
// It never returns "", because an object stored without a type is served as
// application/octet-stream, and both Telegram and Meta refuse to fetch that.
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
		// Reads at most the first 512 bytes, per net/http.
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

func (s *S3Service) GetFileURL(key string) string {
	return fmt.Sprintf("%s/%s", os.Getenv("CLOUDFLARE_R2_ENDPOINT"), key)
}
