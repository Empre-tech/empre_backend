package services

import (
	"errors"
	"fmt"
	"strings"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	// ErrDeletePasswordRequired: la cuenta tiene contraseña y no la mandaron.
	ErrDeletePasswordRequired = errors.New("password required")
	// ErrDeleteWrongPassword: la contraseña no coincide.
	ErrDeleteWrongPassword = errors.New("wrong password")
	// ErrAccountNotFound: la cuenta no existe o ya se eliminó.
	ErrAccountNotFound = errors.New("account not found")
)

// DeletedEmailDomain es el dominio de los correos que reemplazan al real al eliminar una cuenta. .invalid nunca recibe
// correo (RFC 2606).
const DeletedEmailDomain = "deleted.empre.invalid"

// AccountService elimina cuentas a pedido del usuario (lo exigen Google Play y la App Store).
type AccountService struct {
	db *gorm.DB
}

func NewAccountService(db *gorm.DB) *AccountService { return &AccountService{db: db} }

// Delete elimina la cuenta, en una sola transacción:
//   - Sus negocios dejan de verse (borrado lógico del negocio; sus datos quedan fuera de toda consulta).
//   - Se cierran todas sus sesiones (refresh tokens), se quitan sus notificaciones push, sus favoritos y los enlaces
//     para cambiar contraseña pendientes.
//   - Sus datos personales se borran: nombre, correo, teléfono, foto y contraseña. El correo queda libre para crear una
//     cuenta nueva.
//
// Los mensajes y reseñas que escribió se quedan (son también del otro lado de la conversación), firmados como
// «Usuario eliminado». Si la cuenta tiene contraseña, hay que mandarla para confirmar.
func (s *AccountService) Delete(userID uuid.UUID, password string) error {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAccountNotFound
		}
		return err
	}
	if user.PasswordHash != "" {
		if strings.TrimSpace(password) == "" {
			return ErrDeletePasswordRequired
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			return ErrDeleteWrongPassword
		}
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		// Primero sus ofertas y promociones: varias búsquedas las leen sin pasar por el negocio.
		owned := tx.Model(&models.Entity{}).Select("id").Where("owner_id = ?", userID)
		for _, m := range []any{&models.Offer{}, &models.Promotion{}} {
			if err := tx.Where("entity_id IN (?)", owned).Delete(m).Error; err != nil {
				return fmt.Errorf("%T: %w", m, err)
			}
		}
		if err := tx.Where("owner_id = ?", userID).Delete(&models.Entity{}).Error; err != nil {
			return fmt.Errorf("negocios: %w", err)
		}
		for _, m := range []any{&models.RefreshToken{}, &models.PushToken{}, &models.Favorite{}, &models.PasswordResetToken{}} {
			if err := tx.Where("user_id = ?", userID).Delete(m).Error; err != nil {
				return fmt.Errorf("%T: %w", m, err)
			}
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"name":             "Usuario eliminado",
			"email":            fmt.Sprintf("%s@%s", userID, DeletedEmailDomain),
			"phone":            "",
			"profile_media_id": nil,
			"password_hash":    "",
			"role":             models.RoleUser,
		}).Error; err != nil {
			return fmt.Errorf("datos personales: %w", err)
		}
		return tx.Delete(&models.User{}, "id = ?", userID).Error
	})
}
