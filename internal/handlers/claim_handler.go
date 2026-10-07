package handlers

import (
	"net/http"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ClaimHandler struct {
	Service *services.ClaimService
}

func NewClaimHandler(service *services.ClaimService) *ClaimHandler {
	return &ClaimHandler{Service: service}
}

type ClaimRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type RedeemCodeRequest struct {
	Code   string `json:"code" binding:"required"`
	Device string `json:"device"`
}

type RedeemRequest struct {
	Device string `json:"device"`
}

func claimResponse(res *services.ClaimResult) dtos.ClaimResponse {
	return dtos.ClaimResponse{
		ID:        res.Claim.ID,
		OfferID:   res.Claim.OfferID,
		Status:    string(res.Claim.Status),
		Token:     res.Claim.Token,
		ShortCode: res.Claim.ShortCode,
		ClaimedAt: res.Claim.ClaimedAt,
		ExpiresAt: res.Claim.ExpiresAt,
		Offer:     dtos.OfferResponseFrom(res.Offer),
	}
}

// Claim reserves a slot and returns the personal QR (user).
// @Router /api/offers/{id}/claim [post]
func (h *ClaimHandler) Claim(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req ClaimRequest
	_ = c.ShouldBindJSON(&req)

	res, err := h.Service.Claim(offerID, userID, req.Latitude, req.Longitude)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, claimResponse(res))
}

// Get returns the user's claim (QR + time remaining).
// @Router /api/claims/{id} [get]
func (h *ClaimHandler) Get(c *gin.Context) {
	claimID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	claim, err := h.Service.ClaimRepo.FindByID(claimID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Reclamo no encontrado"})
		return
	}
	if claim.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "No autorizado"})
		return
	}

	offer, err := h.Service.OfferRepo.FindByID(claim.OfferID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Oferta no encontrada"})
		return
	}

	c.JSON(http.StatusOK, claimResponse(&services.ClaimResult{Claim: claim, Offer: offer}))
}

// Cancel releases the slot (user).
// @Router /api/claims/{id} [delete]
func (h *ClaimHandler) Cancel(c *gin.Context) {
	claimID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	if err := h.Service.Cancel(claimID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Reclamo cancelado"})
}

// Redeem scans a QR claim and marks it redeemed (business owner).
// @Router /api/claims/{id}/redeem [post]
func (h *ClaimHandler) Redeem(c *gin.Context) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req RedeemRequest
	_ = c.ShouldBindJSON(&req)

	res, err := h.Service.RedeemByID(c.Param("id"), userID, req.Device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"reason": err.Error(), "valid": false})
		return
	}

	c.JSON(http.StatusOK, dtos.RedeemResponse{
		Valid:      true,
		ClaimID:    res.Claim.ID,
		EntityName: res.EntityName,
		OfferTitle: res.Offer.Title,
		ClientName: res.ClientName,
	})
}

// RedeemCode redeems by the 6-char manual code (business owner).
// @Router /api/claims/redeem-code [post]
func (h *ClaimHandler) RedeemCode(c *gin.Context) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req RedeemCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	res, err := h.Service.RedeemByShortCode(req.Code, userID, req.Device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"valid": false, "reason": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dtos.RedeemResponse{
		Valid:      true,
		ClaimID:    res.Claim.ID,
		EntityName: res.EntityName,
		OfferTitle: res.Offer.Title,
		ClientName: res.ClientName,
	})
}

// Confirm marks a redeemed claim as verified by the client (user).
// @Router /api/claims/{id}/confirm [post]
func (h *ClaimHandler) Confirm(c *gin.Context) {
	claimID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	if err := h.Service.Confirm(claimID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Canje confirmado"})
}
