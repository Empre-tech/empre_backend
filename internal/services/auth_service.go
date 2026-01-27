package services

import (
	"errors"

	"empre_backend/config"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	Repo   *repository.UserRepository
	Config *config.Config
}

func NewAuthService(repo *repository.UserRepository, cfg *config.Config) *AuthService {
	return &AuthService{Repo: repo, Config: cfg}
}

func (s *AuthService) Register(user *models.User) error {
	// Check if user exists
	if _, err := s.Repo.FindByEmail(user.Email); err == nil {
		return errors.New("email already registered")
	}

	// Hash Password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashedPassword)

	// Save User
	return s.Repo.Create(user)
}

func (s *AuthService) Login(email, password string) (string, error) {
	user, err := s.Repo.FindByEmail(email)
	if err != nil {
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
