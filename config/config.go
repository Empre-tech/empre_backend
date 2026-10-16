package config

import (
	"errors"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	DBHost         string
	DBUser         string
	DBPassword     string
	DBName         string
	DBPort         string
	DBSSLMode      string
	JWTSecret      string
	// AppEnv = "production" (o GIN_MODE=release) activa las comprobaciones estrictas de arranque.
	AppEnv string
	// Pool de conexiones a PostgreSQL. El pooler de Supabase en modo sesión admite 15: con 100 abiertas
	// devolvía 500 bajo carga (ver PERFORMANCE-REPORT.md). Por defecto 15 abiertas y 5 ociosas.
	DBMaxOpenConns int
	DBMaxIdleConns int
	S3AccessKey    string
	S3SecretKey    string
	S3SessionToken string
	S3Bucket       string
	S3Region       string
	AppURL         string
	SMTPHost       string
	SMTPPort       string
	SMTPUser       string
	SMTPPass       string
	SMTPSender     string
	AIAPIKey       string
	AIBaseURL      string
	AIModel        string

	// Wompi (pasarela de pagos para la suscripción de negocios). Sin
	// WOMPI_PUBLIC_KEY / WOMPI_INTEGRITY_SECRET, los pagos quedan
	// deshabilitados (el resto de la app funciona igual).
	WompiPublicKey      string
	WompiIntegritySecret string
	WompiEventsSecret   string
	// A dónde redirige Wompi al terminar el pago en el widget (una pantalla
	// de la app vía deep link, ej. empre://subscription/success).
	WompiRedirectURL string
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
		DBSSLMode:      getEnv("DB_SSLMODE", "disable"),
		JWTSecret:      getEnv("JWT_SECRET", "changeme"),
		AppEnv:         getEnv("APP_ENV", ""),
		DBMaxOpenConns: getEnvInt("DB_MAX_OPEN_CONNS", DefaultMaxOpenConns),
		DBMaxIdleConns: getEnvInt("DB_MAX_IDLE_CONNS", DefaultMaxIdleConns),
		S3AccessKey:    getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:    getEnv("S3_SECRET_KEY", ""),
		S3SessionToken: getEnv("S3_SESSION_TOKEN", ""),
		S3Bucket:       getEnv("S3_BUCKET", ""),
		S3Region:       getEnv("S3_REGION", "us-east-1"),
		AppURL:         getEnv("APP_URL", "http://localhost:8080"),
		SMTPHost:       getEnv("SMTP_HOST", ""),
		SMTPPort:       getEnv("SMTP_PORT", "587"),
		SMTPUser:       getEnv("SMTP_USER", ""),
		SMTPPass:       getEnv("SMTP_PASS", ""),
		SMTPSender:     getEnv("SMTP_SENDER", ""),
		// OpenCode Zen: un solo API key da acceso a varios modelos (Claude, GPT,
		// Gemini, etc.) a través de un endpoint compatible con la Anthropic
		// Messages API. Sin AI_API_KEY el asistente de creación con IA queda
		// deshabilitado (el resto de la app funciona igual).
		AIAPIKey:  getEnv("AI_API_KEY", ""),
		AIBaseURL: getEnv("AI_BASE_URL", "https://opencode.ai/zen/v1"),
		AIModel:   getEnv("AI_MODEL", "claude-haiku-4-5"),

		WompiPublicKey:       getEnv("WOMPI_PUBLIC_KEY", ""),
		WompiIntegritySecret: getEnv("WOMPI_INTEGRITY_SECRET", ""),
		WompiEventsSecret:    getEnv("WOMPI_EVENTS_SECRET", ""),
		WompiRedirectURL:     getEnv("WOMPI_REDIRECT_URL", "empre://subscription/result"),
	}
}

// Valores por defecto del pool: coinciden con lo que admite el pooler (15) y se pueden cambiar por entorno.
const (
	DefaultMaxOpenConns = 15
	DefaultMaxIdleConns = 5
)

// minJWTSecretLen es el largo mínimo que se considera razonable para firmar tokens.
const minJWTSecretLen = 16

// IsProduction dice si se está en producción: APP_ENV=production o GIN_MODE=release.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production") || strings.EqualFold(os.Getenv("GIN_MODE"), "release")
}

// ValidateJWTSecret revisa el secreto que firma los tokens. En producción un secreto vacío, el valor por
// defecto o uno corto impide arrancar (error); fuera de producción solo avisa (warning) para no estorbar el
// desarrollo local.
func ValidateJWTSecret(secret string, production bool) (warning string, err error) {
	weak := ""
	switch {
	case strings.TrimSpace(secret) == "":
		weak = "JWT_SECRET está vacío"
	case secret == "changeme" || strings.HasPrefix(secret, "cambia-esto"):
		weak = "JWT_SECRET es el valor por defecto o de ejemplo"
	case len(secret) < minJWTSecretLen:
		weak = "JWT_SECRET es demasiado corto (mínimo " + strconv.Itoa(minJWTSecretLen) + " caracteres)"
	}
	if weak == "" {
		return "", nil
	}
	if production {
		return "", errors.New(weak + ": en producción debe ser un secreto largo y aleatorio")
	}
	return weak + " (solo aceptable en desarrollo)", nil
}

// NormalizePool deja el pool en valores válidos: al menos 1 abierta y las ociosas nunca por encima de las abiertas.
func NormalizePool(open, idle int) (int, int) {
	if open < 1 {
		open = DefaultMaxOpenConns
	}
	if idle < 0 {
		idle = 0
	}
	if idle > open {
		idle = open
	}
	return open, idle
}

func getEnvInt(key string, fallback int) int {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		log.Printf("Warning: %s=%q no es un entero; se usa %d", key, value, fallback)
		return fallback
	}
	return n
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
