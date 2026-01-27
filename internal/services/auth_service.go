package services

import (
	"errors"

	"empre_backend/config"
	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	DB     *gorm.DB
	Config *config.Config
}

func NewAuthService(db *gorm.DB, cfg *config.Config) *AuthService {
	return &AuthService{DB: db, Config: cfg}
}

func (s *AuthService) Register(user *models.User) error {
	// Check if user exists
	var existingUser models.User
	if err := s.DB.Where("email = ?", user.Email).First(&existingUser).Error; err == nil {
		return errors.New("email already registered")
	}

	// Hash Password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashedPassword)

	// Save User
	return s.DB.Create(user).Error
}

func (s *AuthService) Login(email, password string) (string, error) {
	var user models.User
	if err := s.DB.Where("email = ?", email).First(&user).Error; err != nil {
		return "", errors.New("Invalid email or password")
	}

	// Verify Password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", errors.New("Invalid email or password")
	}

	// Generate Token
	token, err := utils.GenerateToken(user.ID, string(user.Role), s.Config.JWTSecret)
	if err != nil {
		return "", err
	}

	return token, nil
}
