package services

import (
	"context"
	"fmt"
	"io"
	"log"

	appConfig "empre_backend/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type StorageService struct {
	S3Client *s3.Client
	Bucket   string
}

func NewStorageService(cfg *appConfig.Config) *StorageService {
	var opts []func(*aws_config.LoadOptions) error
	opts = append(opts, aws_config.WithRegion(cfg.S3Region))

	// If keys are provided, use static credentials (useful for local development)
	// Otherwise, LoadDefaultConfig will automatically look for IAM Roles in EC2
	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		creds := credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3SessionToken)
		opts = append(opts, aws_config.WithCredentialsProvider(creds))
	} else {
		log.Println("Note: S3 keys not provided in .env. Using AWS Default Credential Chain (IAM Roles).")
	}

	awsCfg, err := aws_config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		log.Fatal("Unable to load SDK config, ", err)
	}

	client := s3.NewFromConfig(awsCfg)

	return &StorageService{
		S3Client: client,
		Bucket:   cfg.S3Bucket,
	}
}

func (s *StorageService) UploadFile(filename string, body io.Reader, contentType string) error {
	if s.S3Client == nil {
		return fmt.Errorf("storage service not initialized")
	}

	_, err := s.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(filename),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return err
}

func (s *StorageService) GetFile(filename string) (io.ReadCloser, string, error) {
	if s.S3Client == nil {
		return nil, "", fmt.Errorf("storage service not initialized")
	}

	result, err := s.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(filename),
	})
	if err != nil {
		return nil, "", err
	}

	contentType := ""
	if result.ContentType != nil {
		contentType = *result.ContentType
	}

	return result.Body, contentType, nil
}
