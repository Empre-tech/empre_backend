package services

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"empre_backend/internal/models"
)

// Verificar el correo (2026-11-23): las cuentas creadas con contraseña reciben un código de 6 dígitos que se escribe en
// la app. Las de Google ya llegan verificadas (Google lo comprobó). Restablecer la contraseña desde el correo también
// lo verifica: quien abrió el enlace tiene ese buzón.

const (
	emailCodeTTL      = 30 * time.Minute
	emailCodeCooldown = time.Minute
	emailCodeMaxTries = 5
)

var (
	ErrEmailCodeWrong   = errors.New("ese código no es; revísalo en tu correo")
	ErrEmailCodeExpired = errors.New("el código venció o ya no sirve; pide otro")
	ErrEmailCodeTooSoon = errors.New("espera un minuto para pedir otro código")
)

func hashCode(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

func newEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// SendEmailCode manda un código nuevo al correo del usuario. Si ya está verificado no hace nada.
func (s *AuthService) SendEmailCode(userID uuid.UUID) error {
	user, err := s.Repo.FindByID(userID)
	if err != nil {
		return err
	}
	if user.EmailVerifiedAt != nil {
		return nil
	}
	db := s.Repo.DB
	var old models.EmailCode
	if err := db.Where("user_id = ?", userID).First(&old).Error; err == nil && time.Since(old.CreatedAt) < emailCodeCooldown {
		return ErrEmailCodeTooSoon
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	code, err := newEmailCode()
	if err != nil {
		return err
	}
	row := models.EmailCode{UserID: userID, CodeHash: hashCode(code), ExpiresAt: time.Now().Add(emailCodeTTL), CreatedAt: time.Now()}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"code_hash", "expires_at", "attempts", "created_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	return s.Mailer.SendVerificationCode(user.Email, user.Name, code)
}

// VerifyEmail comprueba el código y marca el correo como verificado. Cinco intentos fallidos lo invalidan.
func (s *AuthService) VerifyEmail(userID uuid.UUID, code string) error {
	db := s.Repo.DB
	var row models.EmailCode
	if err := db.Where("user_id = ?", userID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEmailCodeExpired
		}
		return err
	}
	if time.Now().After(row.ExpiresAt) || row.Attempts >= emailCodeMaxTries {
		return ErrEmailCodeExpired
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(row.CodeHash)) != 1 {
		db.Model(&models.EmailCode{}).Where("id = ?", row.ID).Update("attempts", gorm.Expr("attempts + 1"))
		return ErrEmailCodeWrong
	}
	if err := s.markVerified(userID); err != nil {
		return err
	}
	return db.Delete(&models.EmailCode{}, "id = ?", row.ID).Error
}

func (s *AuthService) markVerified(userID uuid.UUID) error {
	return s.Repo.DB.Model(&models.User{}).Where("id = ? AND email_verified_at IS NULL", userID).Update("email_verified_at", time.Now()).Error
}

// sendFirstCode: al registrarse, el primer código sale solo. Un fallo del correo no impide crear la cuenta.
func (s *AuthService) sendFirstCode(userID uuid.UUID) {
	if err := s.SendEmailCode(userID); err != nil {
		log.Printf("verificación de correo: no se pudo mandar el código: %v", err)
	}
}
