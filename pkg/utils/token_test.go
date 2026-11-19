package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "secreto-de-prueba-largo-1234567890"

func TestValidateTokenAcceptsHS256(t *testing.T) {
	id := uuid.New()
	tok, err := GenerateAccessToken(id, "user", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(tok, testSecret)
	if err != nil {
		t.Fatalf("token válido rechazado: %v", err)
	}
	if claims.UserID != id {
		t.Fatalf("user id: %v", claims.UserID)
	}
}

// Un token firmado con otro algoritmo HMAC (aunque use el mismo secreto) se rechaza: solo vale HS256.
func TestValidateTokenRejectsOtherAlgorithms(t *testing.T) {
	claims := Claims{UserID: uuid.New(), Role: "user", RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateToken(tok, testSecret); err == nil {
		t.Fatal("un token HS512 no debería valer")
	}
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateToken(none, testSecret); err == nil {
		t.Fatal("un token sin firma (alg none) no debería valer")
	}
}

func TestValidateTokenRejectsWrongSecretAndExpired(t *testing.T) {
	tok, _ := GenerateAccessToken(uuid.New(), "user", testSecret)
	if _, err := ValidateToken(tok, "otro-secreto-distinto-1234567890"); err == nil {
		t.Fatal("con otro secreto no debería valer")
	}
	old := Claims{UserID: uuid.New(), RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}}
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, old).SignedString([]byte(testSecret))
	if _, err := ValidateToken(expired, testSecret); err == nil {
		t.Fatal("un token vencido no debería valer")
	}
}
