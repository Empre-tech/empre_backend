package services

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"empre_backend/config"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/golang-jwt/jwt/v5"
)

// googleFake firma id_token como Google y publica su llave en un JWKS de prueba.
type googleFake struct {
	key  *rsa.PrivateKey
	srv  *httptest.Server
	hits int
}

func newGoogleFake(t *testing.T) *googleFake {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &googleFake{key: k}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits++
		w.Header().Set("Cache-Control", "public, max-age=3600")
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		}}})
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *googleFake) token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(g.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (g *googleFake) verifier() *GoogleVerifier {
	v := NewGoogleVerifier([]string{"web-client", "android-client"})
	v.CertsURL = g.srv.URL
	return v
}

func goodClaims(email string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": "web-client", "sub": "123",
		"email": email, "email_verified": true, "name": "Rosa Pérez",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}
}

func TestGoogleVerifier(t *testing.T) {
	g := newGoogleFake(t)
	v := g.verifier()

	id, err := v.Verify(g.token(t, goodClaims("Rosa@Gmail.com")))
	if err != nil {
		t.Fatal(err)
	}
	if id.Email != "rosa@gmail.com" || id.Name != "Rosa Pérez" {
		t.Fatalf("identidad = %+v", id)
	}
	// La llave queda en caché.
	if _, err := v.Verify(g.token(t, goodClaims("a@b.co"))); err != nil || g.hits != 1 {
		t.Fatalf("err=%v pedidos de llaves=%d", err, g.hits)
	}

	bad := map[string]func(c jwt.MapClaims){
		"otra audiencia":       func(c jwt.MapClaims) { c["aud"] = "otra-app" },
		"otro emisor":          func(c jwt.MapClaims) { c["iss"] = "https://evil.example" },
		"vencido":              func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"correo sin verificar": func(c jwt.MapClaims) { c["email_verified"] = false },
		"sin correo":           func(c jwt.MapClaims) { delete(c, "email") },
	}
	for name, mutate := range bad {
		c := goodClaims("rosa@gmail.com")
		mutate(c)
		if _, err := v.Verify(g.token(t, c)); !errors.Is(err, ErrGoogleInvalidToken) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Firmado con otra llave.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, goodClaims("rosa@gmail.com"))
	tok.Header["kid"] = "k1"
	forged, _ := tok.SignedString(other)
	if _, err := v.Verify(forged); !errors.Is(err, ErrGoogleInvalidToken) {
		t.Errorf("firma falsa: %v", err)
	}
	// HS256 con la llave pública como secreto (ataque de confusión de algoritmo).
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, goodClaims("rosa@gmail.com"))
	hs.Header["kid"] = "k1"
	hsTok, _ := hs.SignedString([]byte("x"))
	if _, err := v.Verify(hsTok); !errors.Is(err, ErrGoogleInvalidToken) {
		t.Errorf("HS256: %v", err)
	}
	if _, err := NewGoogleVerifier(nil).Verify("x"); !errors.Is(err, ErrGoogleNotConfigured) {
		t.Errorf("sin configurar: %v", err)
	}
}

func TestLoginWithGoogle(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.RefreshToken{}); err != nil {
		t.Fatal(err)
	}
	g := newGoogleFake(t)
	svc := &AuthService{Repo: &repository.UserRepository{DB: db}, RefreshTokenRepo: repository.NewRefreshTokenRepository(db),
		Config: &config.Config{JWTSecret: "secreto-de-prueba-suficientemente-largo"}, Google: g.verifier()}

	// Cuenta nueva: se crea sin contraseña.
	tokens, err := svc.LoginWithGoogle(g.token(t, goodClaims("nueva@gmail.com")), true)
	if err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens=%+v err=%v", tokens, err)
	}
	var created models.User
	if err := db.First(&created, "email = ?", "nueva@gmail.com").Error; err != nil {
		t.Fatal(err)
	}
	if created.PasswordHash != "" || created.Name != "Rosa Pérez" {
		t.Fatalf("cuenta creada = %+v", created)
	}
	// Con contraseña vacía no se puede entrar por el login normal.
	if _, err := svc.Login("nueva@gmail.com", ""); err == nil {
		t.Fatal("entró con contraseña vacía")
	}

	// Cuenta existente (registrada con mayúsculas): entra a la misma, sin duplicar.
	existing := models.User{Name: "Ana", Email: "Ana@Empre.co", PasswordHash: "x"}
	db.Create(&existing)
	if _, err := svc.LoginWithGoogle(g.token(t, goodClaims("ana@empre.co")), false); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.User{}).Where("LOWER(email) = ?", "ana@empre.co").Count(&n)
	if n != 1 {
		t.Fatalf("cuentas con ese correo = %d", n)
	}
	var rt models.RefreshToken
	if err := db.Where("user_id = ?", existing.ID).First(&rt).Error; err != nil {
		t.Fatalf("no abrió sesión en la cuenta existente: %v", err)
	}
}
