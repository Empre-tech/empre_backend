package services

import (
	"errors"
	"fmt"
	"time"

	"empre_backend/config"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	Repo              *repository.UserRepository
	PasswordResetRepo *repository.PasswordResetRepository
	RefreshTokenRepo  *repository.RefreshTokenRepository
	Mailer            MailerService
	Config            *config.Config
}

func NewAuthService(repo *repository.UserRepository, prRepo *repository.PasswordResetRepository, rtRepo *repository.RefreshTokenRepository, mailer MailerService, cfg *config.Config) *AuthService {
	return &AuthService{
		Repo:              repo,
		PasswordResetRepo: prRepo,
		RefreshTokenRepo:  rtRepo,
		Mailer:            mailer,
		Config:            cfg,
	}
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

func (s *AuthService) Login(email, password string) (*utils.TokenResponse, error) {
	user, err := s.Repo.FindByEmail(email)
	if err != nil {
		return nil, errors.New("Invalid email or password")
	}

	// Verify Password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("Invalid email or password")
	}

	// Generate Access Token
	accessToken, err := utils.GenerateAccessToken(user.ID, string(user.Role), s.Config.JWTSecret)
	if err != nil {
		return nil, err
	}

	// Generate Refresh Token
	refreshTokenStr := utils.GenerateRefreshToken()
	refreshToken := &models.RefreshToken{
		UserID:    user.ID,
		Token:     refreshTokenStr,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour), // 30 days
	}

	if err := s.RefreshTokenRepo.Create(refreshToken); err != nil {
		return nil, err
	}

	return &utils.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
	}, nil
}

func (s *AuthService) RefreshToken(tokenStr string) (*utils.TokenResponse, error) {
	// 1. Find the refresh token
	storedToken, err := s.RefreshTokenRepo.FindByToken(tokenStr)
	if err != nil {
		return nil, errors.New("invalid or expired refresh token")
	}

	// 2. Check expiration
	if time.Now().After(storedToken.ExpiresAt) {
		s.RefreshTokenRepo.Delete(storedToken)
		return nil, errors.New("refresh token expired")
	}

	// 3. Find User
	user, err := s.Repo.FindByID(storedToken.UserID)
	if err != nil {
		return nil, errors.New("user not found")
	}

	// 4. ROTATION: Delete the old token
	s.RefreshTokenRepo.Delete(storedToken)

	// 5. Generate NEW Access Token
	newAccessToken, err := utils.GenerateAccessToken(user.ID, string(user.Role), s.Config.JWTSecret)
	if err != nil {
		return nil, err
	}

	// 6. Generate NEW Refresh Token
	newRefreshTokenStr := utils.GenerateRefreshToken()
	newRefreshToken := &models.RefreshToken{
		UserID:    user.ID,
		Token:     newRefreshTokenStr,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour), // 30 days
	}

	if err := s.RefreshTokenRepo.Create(newRefreshToken); err != nil {
		return nil, err
	}

	return &utils.TokenResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshTokenStr,
	}, nil
}

func (s *AuthService) RequestPasswordReset(email string) error {
	user, err := s.Repo.FindByEmail(email)
	if err != nil {
		// We don't want to reveal if an email exists for security reasons,
		// but for now, we'll return nil so the API says "success" even if not found.
		return nil
	}

	// 1. Clean up old tokens for this user
	s.PasswordResetRepo.DeleteByUserID(user.ID)

	// 2. Generate secure token
	token := uuid.New().String()

	// 3. Save token
	resetToken := &models.PasswordResetToken{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour), // 1 hour expiration
	}

	if err := s.PasswordResetRepo.Create(resetToken); err != nil {
		return err
	}

	// 4. Send Email
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.Config.AppURL, token)
	return s.Mailer.SendPasswordReset(user.Email, resetURL)
}

func (s *AuthService) ResetPassword(token, newPassword string) error {
	// 1. Find token
	resetToken, err := s.PasswordResetRepo.FindByToken(token)
	if err != nil {
		return errors.New("invalid or expired token")
	}

	// 2. Check expiration
	if time.Now().After(resetToken.ExpiresAt) {
		s.PasswordResetRepo.Delete(resetToken)
		return errors.New("token expired")
	}

	// 3. Update User Password
	user, err := s.Repo.FindByID(resetToken.UserID)
	if err != nil {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashedPassword)

	if err := s.Repo.Update(user); err != nil {
		return err
	}

	// 4. Revoke token
	return s.PasswordResetRepo.Delete(resetToken)
}
