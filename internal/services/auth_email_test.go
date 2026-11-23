package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"empre_backend/config"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

type captureMailer struct {
	resets []string
	codes  []string
}

func (m *captureMailer) SendPasswordReset(to, url string) error {
	m.resets = append(m.resets, url)
	return nil
}
func (m *captureMailer) SendVerificationCode(to, name, code string) error {
	m.codes = append(m.codes, code)
	return nil
}

// TestEmailVerificationAndReset: al registrarse sale el código; uno equivocado no verifica; el correcto sí. El enlace de
// restablecer va a /restablecer, cambia la contraseña, cierra las sesiones y deja el correo verificado.
func TestEmailVerificationAndReset(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.RefreshToken{}, &models.PasswordResetToken{}, &models.EmailCode{}); err != nil {
		t.Fatal(err)
	}
	mail := &captureMailer{}
	svc := &AuthService{Repo: &repository.UserRepository{DB: db}, RefreshTokenRepo: repository.NewRefreshTokenRepository(db),
		PasswordResetRepo: repository.NewPasswordResetRepository(db), Mailer: mail,
		Config: &config.Config{JWTSecret: "secreto-de-prueba-suficientemente-largo", AppURL: "https://api.empre.com.co/"}}

	u := models.User{Name: "Ana Pérez", Email: "ana@x.co", PasswordHash: "secreta1"}
	if err := svc.Register(&u); err != nil {
		t.Fatal(err)
	}
	if len(mail.codes) != 1 || len(mail.codes[0]) != 6 {
		t.Fatalf("debía salir un código de 6 dígitos: %v", mail.codes)
	}
	if err := svc.SendEmailCode(u.ID); !errors.Is(err, ErrEmailCodeTooSoon) {
		t.Fatalf("pedir otro enseguida: %v", err)
	}
	wrong := "000000"
	if mail.codes[0] == wrong {
		wrong = "111111"
	}
	if err := svc.VerifyEmail(u.ID, wrong); !errors.Is(err, ErrEmailCodeWrong) {
		t.Fatalf("código equivocado: %v", err)
	}
	if err := svc.VerifyEmail(u.ID, mail.codes[0]); err != nil {
		t.Fatal(err)
	}
	var got models.User
	db.First(&got, "id = ?", u.ID)
	if got.EmailVerifiedAt == nil {
		t.Fatal("el correo debía quedar verificado")
	}

	// Restablecer: otra cuenta sin verificar y con una sesión abierta.
	v := models.User{Name: "Luis", Email: "luis@x.co", PasswordHash: "secreta1"}
	if err := svc.Register(&v); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login("luis@x.co", "secreta1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPasswordReset("luis@x.co"); err != nil {
		t.Fatal(err)
	}
	link := mail.resets[0]
	if !strings.HasPrefix(link, "https://api.empre.com.co/restablecer?token=") {
		t.Fatalf("enlace: %s", link)
	}
	if err := svc.ResetPassword(strings.SplitN(link, "token=", 2)[1], "nueva-clave"); err != nil {
		t.Fatal(err)
	}
	var sessions int64
	db.Model(&models.RefreshToken{}).Where("user_id = ?", v.ID).Count(&sessions)
	db.First(&got, "id = ?", v.ID)
	if sessions != 0 || got.EmailVerifiedAt == nil {
		t.Fatalf("sesiones=%d verificado=%v", sessions, got.EmailVerifiedAt)
	}
	if _, err := svc.Login("luis@x.co", "nueva-clave"); err != nil {
		t.Fatalf("entra con la nueva: %v", err)
	}
	_ = time.Now
}
