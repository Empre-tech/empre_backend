package handlers

import (
	"errors"
	"net/http"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	Service *services.AuthService
}

func NewAuthHandler(service *services.AuthService) *AuthHandler {
	return &AuthHandler{Service: service}
}

type RegisterRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Phone    string `json:"phone"`
	// AcceptTerms: marcó «Tengo 18 años o más y acepto la Política de tratamiento de datos y los Términos». Sin esto no
	// se crea la cuenta (Ley 1581 de 2012: autorización previa, expresa e informada).
	AcceptTerms bool `json:"accept_terms"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Register handles user registration
// @Summary Register a new user
// @Description Create a new user account with email and password
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration Info"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 49 {object} map[string]string
// @Router /api/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !req.AcceptTerms {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Para crear tu cuenta tienes que aceptar la Política de tratamiento de datos y los Términos."})
		return
	}
	now := time.Now()
	user := models.User{
		Name:            req.Name,
		Email:           req.Email,
		PasswordHash:    req.Password, // Temp storage before hashing
		Phone:           req.Phone,
		TermsVersion:    services.LegalVersion,
		TermsAcceptedAt: &now,
	}

	if err := h.Service.Register(&user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User registered successfully"})
}

// Login handles user authentication
// @Summary User login
// @Description Authenticate user and return JWT token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login Credentials"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.Service.Login(req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

type GoogleLoginRequest struct {
	IDToken string `json:"id_token" binding:"required"`
	// AcceptTerms: aceptó la política y los términos en la pantalla de registro. Si la cuenta es nueva y no los aceptó
	// aquí, la app los pide antes de dejarla entrar.
	AcceptTerms bool `json:"accept_terms"`
}

// GoogleLogin: POST /api/auth/google {"id_token": "..."} con el token de «Continuar con Google». Devuelve la misma
// sesión que el login normal; si no había cuenta con ese correo, la crea.
func (h *AuthHandler) GoogleLogin(c *gin.Context) {
	var req GoogleLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Falta el token de Google."})
		return
	}
	tokens, err := h.Service.LoginWithGoogle(req.IDToken, req.AcceptTerms)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, tokens)
	case errors.Is(err, services.ErrGoogleNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "El inicio de sesión con Google no está disponible en este momento."})
	case errors.Is(err, services.ErrGoogleInvalidToken):
		c.Error(err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No pudimos verificar tu cuenta de Google. Intenta de nuevo."})
	default:
		internalError(c, err)
	}
}

// RefreshToken handles token refresh requests
// @Summary Refresh access token
// @Description Obtain a new access token using a refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshTokenRequest true "Refresh Token"
// @Success 200 {object} utils.TokenResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokens, err := h.Service.RefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

// SendEmailCode: POST /api/users/me/verify-email/send, manda un código nuevo al correo de la cuenta.
func (h *AuthHandler) SendEmailCode(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	if err := h.Service.SendEmailCode(user); err != nil {
		if errors.Is(err, services.ErrEmailCodeTooSoon) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_soon", "message": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "mail_failed", "message": "No pudimos mandar el correo. Inténtalo en un rato."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

type verifyEmailRequest struct {
	Code string `json:"code" binding:"required,len=6,numeric"`
}

// VerifyEmail: POST /api/users/me/verify-email {"code":"123456"}.
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	var req verifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": "El código tiene 6 números."})
		return
	}
	if err := h.Service.VerifyEmail(user, req.Code); err != nil {
		if errors.Is(err, services.ErrEmailCodeWrong) || errors.Is(err, services.ErrEmailCodeExpired) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad_code", "message": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"verified": true})
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

// RequestPasswordReset handles forgot password requests
// @Summary Request password reset
// @Description Send a reset link to the user's email
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ForgotPasswordRequest true "User Email"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/auth/password-reset/request [post]
func (h *AuthHandler) RequestPasswordReset(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.Service.RequestPasswordReset(req.Email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a reset link has been sent"})
}

// ResetPassword handles password reset execution
// @Summary Reset password
// @Description Update password using a valid reset token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "Token and New Password"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/auth/password-reset/reset [post]
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.Service.ResetPassword(req.Token, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}
