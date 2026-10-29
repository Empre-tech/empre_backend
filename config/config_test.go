package config

import (
	"testing"
	"time"
)

func TestValidateJWTSecret(t *testing.T) {
	cases := []struct {
		name       string
		secret     string
		production bool
		wantErr    bool
		wantWarn   bool
	}{
		{"secreto largo en producción", "un-secreto-largo-y-aleatorio-123456", true, false, false},
		{"secreto largo en desarrollo", "un-secreto-largo-y-aleatorio-123456", false, false, false},
		{"valor por defecto en producción", "changeme", true, true, false},
		{"valor por defecto en desarrollo", "changeme", false, false, true},
		{"ejemplo de .env.example en producción", "cambia-esto-por-un-secreto-largo-y-aleatorio", true, true, false},
		{"vacío en producción", "", true, true, false},
		{"solo espacios en producción", "   ", true, true, false},
		{"vacío en desarrollo", "", false, false, true},
		{"corto en producción", "abc123", true, true, false},
		{"justo el mínimo en producción", "0123456789abcdef", true, false, false},
		{"un carácter menos del mínimo", "0123456789abcde", true, true, false},
	}
	for _, c := range cases {
		warn, err := ValidateJWTSecret(c.secret, c.production)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: error=%v, quería error=%v", c.name, err, c.wantErr)
		}
		if (warn != "") != c.wantWarn {
			t.Errorf("%s: aviso=%q, quería aviso=%v", c.name, warn, c.wantWarn)
		}
	}
}

func TestIsProduction(t *testing.T) {
	t.Setenv("GIN_MODE", "")
	if (&Config{AppEnv: ""}).IsProduction() {
		t.Error("sin APP_ENV ni GIN_MODE no es producción")
	}
	if !(&Config{AppEnv: "Production"}).IsProduction() {
		t.Error("APP_ENV=Production (sin distinguir mayúsculas) es producción")
	}
	if (&Config{AppEnv: "staging"}).IsProduction() {
		t.Error("staging no es producción")
	}
	t.Setenv("GIN_MODE", "release")
	if !(&Config{}).IsProduction() {
		t.Error("GIN_MODE=release es producción")
	}
}

func TestNormalizePool(t *testing.T) {
	cases := []struct{ open, idle, wantOpen, wantIdle int }{
		{15, 5, 15, 5},
		{0, 5, DefaultMaxOpenConns, 5},  // abiertas inválidas: por defecto
		{-3, 2, DefaultMaxOpenConns, 2}, // negativas: por defecto
		{10, 50, 10, 10},                // las ociosas nunca pasan de las abiertas
		{20, -1, 20, 0},                 // ociosas negativas: cero
	}
	for _, c := range cases {
		o, i := NormalizePool(c.open, c.idle)
		if o != c.wantOpen || i != c.wantIdle {
			t.Errorf("NormalizePool(%d,%d) = %d,%d; quería %d,%d", c.open, c.idle, o, i, c.wantOpen, c.wantIdle)
		}
	}
	if DefaultMaxIdleConns != DefaultMaxOpenConns {
		t.Errorf("las inactivas deben ser tantas como las abiertas (3.54): %d y %d", DefaultMaxIdleConns, DefaultMaxOpenConns)
	}
	if DefaultMaxOpenConns != 15 {
		t.Errorf("el pool por defecto debe ser 15 (el del pooler), es %d", DefaultMaxOpenConns)
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Setenv("EMPRE_TEST_INT", "42")
	if got := getEnvInt("EMPRE_TEST_INT", 7); got != 42 {
		t.Errorf("valor válido: %d", got)
	}
	t.Setenv("EMPRE_TEST_INT", " 8 ")
	if got := getEnvInt("EMPRE_TEST_INT", 7); got != 8 {
		t.Errorf("con espacios: %d", got)
	}
	t.Setenv("EMPRE_TEST_INT", "muchos")
	if got := getEnvInt("EMPRE_TEST_INT", 7); got != 7 {
		t.Errorf("no numérico debe dar el valor por defecto: %d", got)
	}
	if got := getEnvInt("EMPRE_TEST_INT_AUSENTE", 9); got != 9 {
		t.Errorf("ausente debe dar el valor por defecto: %d", got)
	}
}

func TestWarmCountAndIdleTime(t *testing.T) {
	cases := []struct{ warm, idle, want int }{
		{-1, 15, 15}, // por omisión: todas las inactivas
		{6, 15, 6},
		{40, 15, 15}, // nunca más que las inactivas
		{0, 15, 0},   // 0 = sin precalentar
	}
	for _, c := range cases {
		if got := WarmCount(c.warm, c.idle); got != c.want {
			t.Errorf("WarmCount(%d,%d) = %d; quería %d", c.warm, c.idle, got, c.want)
		}
	}
	if IdleTime(0) != 15*time.Minute || IdleTime(3) != 3*time.Minute {
		t.Errorf("IdleTime: %v, %v", IdleTime(0), IdleTime(3))
	}
}
