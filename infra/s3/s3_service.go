package s3

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"

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

func (s *S3Service) UploadFile(key string, data []byte) error {
	_, err := s.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		fmt.Println("Error uploading file:", err)
	}

	return err
}

func (s *S3Service) GetFileURL(key string) string {
	return fmt.Sprintf("%s/%s", os.Getenv("CLOUDFLARE_R2_ENDPOINT"), key)
}
