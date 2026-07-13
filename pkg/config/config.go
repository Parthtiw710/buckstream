package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	AllowedDomains           string
	RootDomain               string
	S3ByIAM                  bool
	GCSByIAM                 bool
	S3CompatibleByToken      bool
	S3CompatibleEndpoint     string
	S3CompatibleRegion       string
	S3CompatibleAccessKey    string
	S3CompatibleAccessSecret string
	BucketName               string
	DeployToken              string
	UploadToken              string
}

func LoadConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	return &Config{
		AllowedDomains:           os.Getenv("ALLOWED_DOMAINS"),
		RootDomain:               os.Getenv("ROOT_DOMAIN"),
		S3ByIAM:                  parseBoolEnv("S3_BY_IAM", false),
		GCSByIAM:                 parseBoolEnv("GCS_BY_IAM", false),
		S3CompatibleByToken:      parseBoolEnv("S3_COMPATIBLE_BY_TOKEN", false),
		S3CompatibleEndpoint:     os.Getenv("S3_COMPATIBLE_ENDPOINT"),
		S3CompatibleRegion:       getEnv("S3_COMPATIBLE_REGION", "us-east-1"),
		S3CompatibleAccessKey:    os.Getenv("S3_COMPATIBLE_ACCESS_KEY"),
		S3CompatibleAccessSecret: os.Getenv("S3_COMPATIBLE_ACCESS_SECRET"),
		BucketName:               os.Getenv("BUCKET_NAME"),
		DeployToken:              os.Getenv("DEPLOY_TOKEN"),
		UploadToken:              os.Getenv("UPLOAD_TOKEN"),
	}
}

func parseBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
