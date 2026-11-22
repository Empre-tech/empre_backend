package services

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GoogleCertsURL es donde Google publica las llaves públicas con las que firma los id_token.
const GoogleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

var (
	// ErrGoogleNotConfigured: falta GOOGLE_CLIENT_IDS en el servidor.
	ErrGoogleNotConfigured = errors.New("google sign-in not configured")
	// ErrGoogleInvalidToken: el token no es de Google, no es para Empre, venció o el correo no está verificado.
	ErrGoogleInvalidToken = errors.New("invalid google token")
)

// GoogleIdentity es lo que Empre usa del id_token de Google.
type GoogleIdentity struct {
	Subject string
	Email   string
	Name    string
	Picture string
}

type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // Google lo manda como bool (a veces como "true")
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	jwt.RegisteredClaims
}

// GoogleVerifier comprueba id_token de «Continuar con Google»: firma RS256 con las llaves públicas de Google (en
// caché lo que diga Cache-Control), emisor de Google, audiencia = uno de los client ID de Empre y correo verificado.
type GoogleVerifier struct {
	ClientIDs []string
	CertsURL  string
	Client    *http.Client
	Now       func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

func NewGoogleVerifier(clientIDs []string) *GoogleVerifier {
	return &GoogleVerifier{ClientIDs: clientIDs, CertsURL: GoogleCertsURL, Client: &http.Client{Timeout: 10 * time.Second}, Now: time.Now}
}

func (v *GoogleVerifier) Enabled() bool { return v != nil && len(v.ClientIDs) > 0 }

// Verify valida el id_token y devuelve la identidad.
func (v *GoogleVerifier) Verify(idToken string) (*GoogleIdentity, error) {
	if !v.Enabled() {
		return nil, ErrGoogleNotConfigured
	}
	claims := &googleClaims{}
	_, err := jwt.ParseWithClaims(idToken, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.key(kid)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.Now),
		jwt.WithLeeway(time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGoogleInvalidToken, err)
	}
	if claims.Issuer != "accounts.google.com" && claims.Issuer != "https://accounts.google.com" {
		return nil, fmt.Errorf("%w: emisor %q", ErrGoogleInvalidToken, claims.Issuer)
	}
	if !v.audienceOK(claims.Audience) {
		return nil, fmt.Errorf("%w: audiencia %v", ErrGoogleInvalidToken, claims.Audience)
	}
	verified := claims.EmailVerified == true || claims.EmailVerified == "true"
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" || !verified {
		return nil, fmt.Errorf("%w: correo sin verificar", ErrGoogleInvalidToken)
	}
	return &GoogleIdentity{Subject: claims.Subject, Email: email, Name: strings.TrimSpace(claims.Name), Picture: claims.Picture}, nil
}

func (v *GoogleVerifier) audienceOK(aud jwt.ClaimStrings) bool {
	for _, a := range aud {
		for _, id := range v.ClientIDs {
			if a == id {
				return true
			}
		}
	}
	return false
}

func (v *GoogleVerifier) key(kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok && v.Now().Before(v.expires) {
		return k, nil
	}
	// Llave desconocida o caché vencida: Google rota las llaves, se vuelven a pedir.
	if err := v.refresh(); err != nil {
		return nil, err
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("llave %q desconocida", kid)
}

func (v *GoogleVerifier) refresh() error {
	resp, err := v.Client.Get(v.CertsURL)
	if err != nil {
		return fmt.Errorf("llaves de google: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llaves de google: http %d", resp.StatusCode)
	}
	var body struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("llaves de google: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range body.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("llaves de google: respuesta sin llaves")
	}
	ttl := time.Hour
	for _, part := range strings.Split(resp.Header.Get("Cache-Control"), ",") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(part), "max-age="); ok {
			if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
				ttl = time.Duration(secs) * time.Second
			}
		}
	}
	v.keys, v.expires = keys, v.Now().Add(ttl)
	return nil
}
