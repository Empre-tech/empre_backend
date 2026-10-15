package handlers

import (
	"errors"
	"net/http"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
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
		if errors.Is(err, services.ErrOwnerClaim) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
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
	status, body := redeemOutcome(res, err)
	c.JSON(status, body)
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
	status, body := redeemOutcome(res, err)
	c.JSON(status, body)
}

// redeemOutcome turns the service result into the HTTP answer. A rejection
// (already redeemed, expired, wrong business...) is an expected outcome and
// answers 200 with valid=false and a reason_code; any other error is a real
// failure and answers 500.
func redeemOutcome(res *services.ClaimResult, err error) (int, any) {
	var rej *services.RedeemRejection
	if errors.As(err, &rej) {
		return http.StatusOK, redeemRejectedResponse(rej)
	}
	if err != nil {
		return http.StatusInternalServerError, gin.H{"error": err.Error()}
	}
	return http.StatusOK, redeemValidResponse(res)
}

func slotsOf(o *models.Offer) (left, total *int) {
	l := o.TotalSlots - o.UsedSlots
	if l < 0 {
		l = 0
	}
	t := o.TotalSlots
	return &l, &t
}

func redeemValidResponse(res *services.ClaimResult) dtos.RedeemResponse {
	left, total := slotsOf(res.Offer)
	out := dtos.RedeemResponse{
		Valid:           true,
		ClaimID:         &res.Claim.ID,
		EntityName:      res.EntityName,
		OfferTitle:      res.Offer.Title,
		OfferPrice:      res.Offer.Price,
		OfferConditions: res.Offer.Conditions,
		ClientName:      dtos.ShortName(res.ClientName),
		ClaimedAt:       &res.Claim.ClaimedAt,
		ExpiresAt:       &res.Claim.ExpiresAt,
		RedeemedAt:      res.Claim.RedeemedAt,
		SlotsLeft:       left,
		SlotsTotal:      total,
	}
	return out
}

func redeemRejectedResponse(rej *services.RedeemRejection) dtos.RedeemResponse {
	out := dtos.RedeemResponse{Valid: false, Reason: rej.Message, ReasonCode: string(rej.Code)}
	if rej.Claim != nil {
		out.ClaimID = &rej.Claim.ID
		out.ClaimedAt = &rej.Claim.ClaimedAt
		out.ExpiresAt = &rej.Claim.ExpiresAt
		out.RedeemedAt = rej.Claim.RedeemedAt
		out.ClientName = dtos.ShortName(rej.ClientName)
	}
	if rej.Offer != nil {
		left, total := slotsOf(rej.Offer)
		out.OfferTitle = rej.Offer.Title
		out.OfferPrice = rej.Offer.Price
		out.OfferConditions = rej.Offer.Conditions
		out.SlotsLeft = left
		out.SlotsTotal = total
	}
	return out
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
