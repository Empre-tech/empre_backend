package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	DBHost         string
	DBUser         string
	DBPassword     string
	DBName         string
	DBPort         string
	JWTSecret      string
	S3AccessKey    string
	S3SecretKey    string
	S3SessionToken string
	S3Bucket       string
	S3Region       string
}

func LoadConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, using system environment variables")
	}

	return &Config{
		Port:           getEnv("PORT", "8080"),
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBUser:         getEnv("DB_USER", "postgres"),
		DBPassword:     getEnv("DB_PASSWORD", "postgres"),
		DBName:         getEnv("DB_NAME", "empre_db"),
		DBPort:         getEnv("DB_PORT", "5432"),
		JWTSecret:      getEnv("JWT_SECRET", "changeme"),
		S3AccessKey:    getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:    getEnv("S3_SECRET_KEY", ""),
		S3SessionToken: getEnv("S3_SESSION_TOKEN", ""),
		S3Bucket:       getEnv("S3_BUCKET", ""),
		S3Region:       getEnv("S3_REGION", "us-east-1"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
