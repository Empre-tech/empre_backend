package services

import (
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// maxClaimsPerDay caps how many claims a single user can make in a day
// (anti-spam, per the antifraud rules).
const maxClaimsPerDay = 10

// shortCodeAlphabet avoids visually ambiguous characters (0/O, 1/I/L).
const shortCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

type ClaimService struct {
	ClaimRepo     *repository.ClaimRepository
	OfferRepo     *repository.OfferRepository
	EntityRepo    *repository.EntityRepository
	UserRepo      *repository.UserRepository
	SignalService *SignalService
}

func NewClaimService(claimRepo *repository.ClaimRepository, offerRepo *repository.OfferRepository, entityRepo *repository.EntityRepository, userRepo *repository.UserRepository, signalService *SignalService) *ClaimService {
	return &ClaimService{ClaimRepo: claimRepo, OfferRepo: offerRepo, EntityRepo: entityRepo, UserRepo: userRepo, SignalService: signalService}
}

func generateShortCode() (string, error) {
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(shortCodeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = shortCodeAlphabet[n.Int64()]
	}
	return string(b), nil
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ClaimResult pairs the created claim with the offer it belongs to (for the
// QR screen and the offer info). ClientName is filled on redemption so the
// cashier can verify who is claiming at the counter.
type ClaimResult struct {
	Claim      *models.OfferClaim
	Offer      *models.Offer
	ClientName string
	EntityName string
}

// Claim reserves a slot and creates a personal QR for the user. lat/lng are
// optional (best effort radius check); 0/0 skips the distance validation.
func (s *ClaimService) Claim(offerID, userID uuid.UUID, lat, lng float64) (*ClaimResult, error) {
	offer, err := s.OfferRepo.FindByID(offerID)
	if err != nil {
		return nil, errors.New("oferta no encontrada")
	}

	now := utils.NowInCartagena()
	if !offer.EndsAt.After(now) {
		return nil, errors.New("oferta finalizada")
	}
	if offer.UsedSlots >= offer.TotalSlots {
		return nil, errors.New("oferta agotada")
	}

	// Repetition policy.
	if offer.RepeatPolicy == models.RepeatDaily {
		n, err := s.ClaimRepo.CountRedeemedByOfferAndUserSince(offerID, userID, startOfDay(now))
		if err != nil {
			return nil, err
		}
		if n >= 1 {
			return nil, errors.New("ya canjeaste esta oferta hoy")
		}
	} else {
		n, err := s.ClaimRepo.CountActiveByOfferAndUser(offerID, userID, now)
		if err != nil {
			return nil, err
		}
		if n >= 1 {
			return nil, errors.New("ya reclamaste esta oferta")
		}
	}

	// Daily claim cap.
	n, err := s.ClaimRepo.CountClaimsByUserSince(userID, startOfDay(now))
	if err != nil {
		return nil, err
	}
	if n >= maxClaimsPerDay {
		return nil, errors.New("alcanzaste el límite diario de reclamos")
	}

	// Radius check (best effort).
	if (lat != 0 || lng != 0) && offer.RadiusMeters > 0 {
		entity, err := s.EntityRepo.FindByID(offer.EntityID)
		if err != nil {
			return nil, errors.New("negocio no encontrado")
		}
		if utils.DistanceMeters(lat, lng, entity.Latitude, entity.Longitude) > float64(offer.RadiusMeters) {
			return nil, errors.New("debes estar más cerca del negocio para reclamar")
		}
	}

	// Reserve the slot atomically.
	ok, err := s.OfferRepo.ReserveSlot(offer.ID, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("oferta agotada")
	}

	token := uuid.New().String()
	code, err := generateShortCode()
	if err != nil {
		_ = s.OfferRepo.ReleaseSlot(offer.ID)
		return nil, err
	}

	window := offerTypeRules[offer.Type].claimWindow
	expires := now.Add(window)
	if expires.After(offer.EndsAt) {
		expires = offer.EndsAt
	}

	claim := &models.OfferClaim{
		ID:        uuid.New(),
		OfferID:   offer.ID,
		UserID:    userID,
		EntityID:  offer.EntityID,
		Status:    models.ClaimClaimed,
		Token:     token,
		ShortCode: code,
		ClaimedAt: now,
		ExpiresAt: expires,
	}
	if err := s.ClaimRepo.Create(claim); err != nil {
		_ = s.OfferRepo.ReleaseSlot(offer.ID)
		return nil, err
	}

	return &ClaimResult{Claim: claim, Offer: offer}, nil
}

// Cancel releases the slot and marks the claim canceled.
func (s *ClaimService) Cancel(claimID, userID uuid.UUID) error {
	claim, err := s.ClaimRepo.FindByID(claimID)
	if err != nil {
		return errors.New("reclamo no encontrado")
	}
	if claim.UserID != userID {
		return errors.New("no autorizado")
	}
	if claim.Status != models.ClaimClaimed {
		return errors.New("el reclamo ya no está activo")
	}

	now := utils.NowInCartagena()
	ok, err := s.ClaimRepo.Transition(claim.ID, []models.ClaimStatus{models.ClaimClaimed}, models.ClaimCanceled, now)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("el reclamo ya expiró")
	}
	_ = s.OfferRepo.ReleaseSlot(claim.OfferID)
	return nil
}

// RedeemByToken validates and consumes a claim from its QR token. The
// redeemer must own the business (MVP: only the account owner scans).
func (s *ClaimService) RedeemByToken(token string, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	claim, err := s.ClaimRepo.FindByToken(token)
	if err != nil {
		return nil, errors.New("código no válido")
	}
	return s.redeem(claim, redeemerID, device)
}

// RedeemByShortCode is the manual fallback for when the QR can't be scanned.
func (s *ClaimService) RedeemByShortCode(code string, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	claim, err := s.ClaimRepo.FindByShortCode(code)
	if err != nil {
		return nil, errors.New("código no válido")
	}
	return s.redeem(claim, redeemerID, device)
}

// RedeemByID redeems a claim looked up directly by id (QR token decoded on
// the client, or a direct scan flow).
func (s *ClaimService) RedeemByID(claimID string, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	id, err := uuid.Parse(claimID)
	if err != nil {
		return nil, errors.New("código no válido")
	}
	claim, err := s.ClaimRepo.FindByID(id)
	if err != nil {
		return nil, errors.New("código no válido")
	}
	return s.redeem(claim, redeemerID, device)
}

func (s *ClaimService) redeem(claim *models.OfferClaim, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	entity, err := s.EntityRepo.FindByID(claim.EntityID)
	if err != nil {
		return nil, errors.New("negocio no encontrado")
	}
	if entity.OwnerID != redeemerID {
		return nil, errors.New("este código no es de tu negocio")
	}

	now := utils.NowInCartagena()

	switch claim.Status {
	case models.ClaimRedeemed:
		return nil, errors.New("ya canjeado")
	case models.ClaimCanceled:
		return nil, errors.New("reclamo cancelado")
	case models.ClaimExpired:
		return nil, errors.New("reclamo expirado")
	case models.ClaimClaimed:
		if !claim.ExpiresAt.After(now) {
			if ok, _ := s.ClaimRepo.MarkExpired(claim.ID, now); ok {
				_ = s.OfferRepo.ReleaseSlot(claim.OfferID)
			}
			return nil, errors.New("reclamo expirado")
		}
	}

	ok, err := s.ClaimRepo.Redeem(claim.ID, redeemerID, device, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("ya canjeado o expirado")
	}

	offer, err := s.OfferRepo.FindByID(claim.OfferID)
	if err != nil {
		return nil, err
	}

	// Refresh the claim so the response carries the audit fields.
	redeemed, err := s.ClaimRepo.FindByID(claim.ID)
	if err != nil {
		return nil, err
	}

	clientName := ""
	if user, err := s.UserRepo.FindByID(claim.UserID); err == nil {
		clientName = user.Name
	}

	if s.SignalService != nil {
		s.SignalService.Record(claim.EntityID, models.SignalRedeem)
	}

	return &ClaimResult{Claim: redeemed, Offer: offer, ClientName: clientName, EntityName: entity.Name}, nil
}

// Confirm marks a redeemed claim as verified by the client (antifraud signal).
func (s *ClaimService) Confirm(claimID, userID uuid.UUID) error {
	claim, err := s.ClaimRepo.FindByID(claimID)
	if err != nil {
		return errors.New("reclamo no encontrado")
	}
	if claim.UserID != userID {
		return errors.New("no autorizado")
	}
	if claim.Status != models.ClaimRedeemed {
		return errors.New("el reclamo aún no ha sido canjeado")
	}
	if claim.VerifiedByClient {
		return nil
	}
	return s.ClaimRepo.DB.Model(&models.OfferClaim{}).Where("id = ?", claim.ID).Update("verified_by_client", true).Error
}
