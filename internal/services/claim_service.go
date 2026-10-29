package services

import (
	"crypto/rand"
	"errors"
	"log"
	"math/big"
	"strings"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/observability"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// maxClaimsPerDay caps how many claims a single user can make in a day
// (anti-spam, per the antifraud rules).
const maxClaimsPerDay = 10

// shortCodeAlphabet avoids visually ambiguous characters (0/O, 1/I/L).
const shortCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// ErrOwnerClaim se devuelve cuando el dueño intenta reclamar una oferta de su
// propio negocio; el handler lo mapea a 403.
var ErrOwnerClaim = errors.New("no puedes reclamar ofertas de tu propio negocio")

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

// RedeemReason is the machine-readable cause of a rejected redemption. The
// scanner picks its screen from it; Message stays the human text.
type RedeemReason string

const (
	ReasonAlreadyRedeemed RedeemReason = "ALREADY_REDEEMED"
	ReasonExpired         RedeemReason = "EXPIRED"
	ReasonCanceled        RedeemReason = "CANCELED"
	ReasonWrongBusiness   RedeemReason = "WRONG_BUSINESS"
	ReasonNotFound        RedeemReason = "NOT_FOUND"
)

// RedeemRejection is an expected outcome of scanning a code that can't be
// redeemed (not a failure of the system). Claim, Offer and ClientName give
// context for the business's own claims; they are nil/empty for
// WRONG_BUSINESS and NOT_FOUND so nothing of another business leaks.
type RedeemRejection struct {
	Code       RedeemReason
	Message    string
	Claim      *models.OfferClaim
	Offer      *models.Offer
	ClientName string
}

func (e *RedeemRejection) Error() string { return e.Message }

// reject builds a rejection for one of this business's own claims, with the
// context the scanner shows (what, who, when). Lookups are best effort.
func (s *ClaimService) reject(code RedeemReason, message string, claim *models.OfferClaim) *RedeemRejection {
	rej := &RedeemRejection{Code: code, Message: message, Claim: claim}
	if claim == nil {
		return rej
	}
	if offer, err := s.OfferRepo.FindByID(claim.OfferID); err == nil {
		rej.Offer = offer
	}
	if user, err := s.UserRepo.FindByID(claim.UserID); err == nil {
		rej.ClientName = user.Name
	}
	return rej
}

// rejectionForState maps a claim that can no longer be redeemed to its
// rejection, from the status it has right now.
func (s *ClaimService) rejectionForState(claim *models.OfferClaim) *RedeemRejection {
	switch claim.Status {
	case models.ClaimRedeemed:
		return s.reject(ReasonAlreadyRedeemed, "ya canjeado", claim)
	case models.ClaimCanceled:
		return s.reject(ReasonCanceled, "reclamo cancelado", claim)
	default:
		return s.reject(ReasonExpired, "reclamo expirado", claim)
	}
}

var errCodeNotFound = func() *RedeemRejection {
	return &RedeemRejection{Code: ReasonNotFound, Message: "código no válido"}
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

	// El dueño no reclama ofertas de su propio negocio (solo escanea y canjea).
	// Se valida antes de reservar el cupo para no consumirlo y luego fallar.
	entity, err := s.EntityRepo.FindByID(offer.EntityID)
	if err != nil {
		return nil, errors.New("negocio no encontrado")
	}
	if entity.OwnerID == userID {
		return nil, ErrOwnerClaim
	}

	now := utils.NowInCartagena()

	// Un reclamo vencido del mismo usuario que el barrido periódico aún no
	// alcanzó no debe bloquear este ni seguir ocupando su cupo.
	expired, err := s.ClaimRepo.ExpireOverdueFor(offerID, userID, now)
	if err != nil {
		return nil, err
	}
	if expired > 0 {
		if offer, err = s.OfferRepo.FindByID(offerID); err != nil {
			return nil, errors.New("oferta no encontrada")
		}
	}

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
		// Un solo reclamo abierto a la vez: sin esto un usuario podría
		// acaparar varios cupos de la misma oferta.
		open, err := s.ClaimRepo.CountOpenByOfferAndUser(offerID, userID, now)
		if err != nil {
			return nil, err
		}
		if open >= 1 {
			return nil, errors.New("ya reclamaste esta oferta")
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

	// Radius check (best effort). `entity` ya está cargada arriba.
	if (lat != 0 || lng != 0) && offer.RadiusMeters > 0 {
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

	window := offer.Type.ClaimWindow()
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
		// Dos reclamos simultáneos del mismo usuario pasan los conteos de
		// arriba a la vez; el índice único deja entrar solo uno.
		if repository.IsDuplicateOpenClaim(err) {
			return nil, errors.New("ya reclamaste esta oferta")
		}
		return nil, err
	}

	return &ClaimResult{Claim: claim, Offer: offer, EntityName: entity.Name}, nil
}

// mineWindow y maxMine acotan «mis reclamos»: lo de las últimas 48 h (los
// reclamos abiertos duran como mucho 24 h) y a lo sumo 30.
const (
	mineWindow = 48 * time.Hour
	maxMine    = 30
)

// Describe completa un reclamo con lo que se muestra de él: la oferta y el
// nombre del negocio.
func (s *ClaimService) Describe(claim *models.OfferClaim) (*ClaimResult, error) {
	offer, err := s.OfferRepo.FindByID(claim.OfferID)
	if err != nil {
		return nil, errors.New("oferta no encontrada")
	}
	names, err := s.ClaimRepo.EntityNames([]uuid.UUID{claim.EntityID})
	if err != nil {
		return nil, err
	}
	return &ClaimResult{Claim: claim, Offer: offer, EntityName: names[claim.EntityID]}, nil
}

// ListMine devuelve los reclamos recientes del usuario, los más nuevos primero,
// con su oferta y el nombre del negocio. Los reclamos de otras personas nunca
// aparecen. Reclamos cuya oferta ya no existe se omiten.
func (s *ClaimService) ListMine(userID uuid.UUID) ([]*ClaimResult, error) {
	now := utils.NowInCartagena()
	claims, err := s.ClaimRepo.FindRecentByUser(userID, now.Add(-mineWindow), maxMine)
	if err != nil {
		return nil, err
	}
	offerIDs := make([]uuid.UUID, 0, len(claims))
	entityIDs := make([]uuid.UUID, 0, len(claims))
	for i := range claims {
		offerIDs = append(offerIDs, claims[i].OfferID)
		entityIDs = append(entityIDs, claims[i].EntityID)
	}
	offers, err := s.OfferRepo.FindByIDs(offerIDs)
	if err != nil {
		return nil, err
	}
	names, err := s.ClaimRepo.EntityNames(entityIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*ClaimResult, 0, len(claims))
	for i := range claims {
		offer, ok := offers[claims[i].OfferID]
		if !ok {
			continue
		}
		out = append(out, &ClaimResult{Claim: &claims[i], Offer: offer, EntityName: names[claims[i].EntityID]})
	}
	return out, nil
}

// RunExpirySweeper expires lapsed claims and releases their slots every
// `interval` until `stop` is closed, so no-shows don't leave an offer looking
// sold out (Backend v2 §5.3, §10).
func (s *ClaimService) RunExpirySweeper(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if n, err := s.ClaimRepo.ExpireOverdue(utils.NowInCartagena()); err != nil {
			observability.Default.Inc("job_error_claim_expiry")
			log.Printf("Reclamos: no se pudieron expirar los vencidos: %v", err)
		} else if n > 0 {
			log.Printf("Reclamos: %d vencidos expirados y sus cupos liberados", n)
		}
		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
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
		// Venció antes de cancelar: expirarlo libera el cupo igualmente.
		_, _ = s.ClaimRepo.ExpireOverdueFor(claim.OfferID, claim.UserID, now)
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
		return nil, errCodeNotFound()
	}
	return s.redeem(claim, redeemerID, device)
}

// RedeemByShortCode is the manual fallback for when the QR can't be scanned.
func (s *ClaimService) RedeemByShortCode(code string, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	claim, err := s.ClaimRepo.FindByShortCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, errCodeNotFound()
	}
	return s.redeem(claim, redeemerID, device)
}

// RedeemByID redeems a claim looked up directly by id (QR token decoded on
// the client, or a direct scan flow).
func (s *ClaimService) RedeemByID(claimID string, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	id, err := uuid.Parse(claimID)
	if err != nil {
		return nil, errCodeNotFound()
	}
	claim, err := s.ClaimRepo.FindByID(id)
	if err != nil {
		return nil, errCodeNotFound()
	}
	return s.redeem(claim, redeemerID, device)
}

func (s *ClaimService) redeem(claim *models.OfferClaim, redeemerID uuid.UUID, device string) (*ClaimResult, error) {
	entity, err := s.EntityRepo.FindByID(claim.EntityID)
	if err != nil {
		return nil, errCodeNotFound()
	}
	if entity.OwnerID != redeemerID {
		// Nada del otro negocio en la respuesta: ni oferta ni cliente.
		return nil, &RedeemRejection{Code: ReasonWrongBusiness, Message: "este código no es de tu negocio"}
	}

	now := utils.NowInCartagena()

	switch claim.Status {
	case models.ClaimRedeemed, models.ClaimCanceled, models.ClaimExpired:
		return nil, s.rejectionForState(claim)
	case models.ClaimClaimed:
		if !claim.ExpiresAt.After(now) {
			if ok, _ := s.ClaimRepo.MarkExpired(claim.ID, now); ok {
				_ = s.OfferRepo.ReleaseSlot(claim.OfferID)
			}
			return nil, s.reject(ReasonExpired, "reclamo expirado", claim)
		}
	}

	ok, err := s.ClaimRepo.Redeem(claim.ID, redeemerID, device, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		// Perdió una carrera (otro escaneo canjeó o venció justo ahora): se
		// vuelve a leer el estado real para decir cuál fue.
		current, err := s.ClaimRepo.FindByID(claim.ID)
		if err != nil {
			return nil, err
		}
		if current.Status == models.ClaimClaimed {
			current.Status = models.ClaimExpired
		}
		return nil, s.rejectionForState(current)
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

	// La señal de popularidad NO se registra aquí: un canje cuenta para el
	// ranking solo cuando el cliente lo confirma (ver Confirm). Así un negocio
	// no puede inflar su posición canjeando con cuentas falsas.

	return &ClaimResult{Claim: redeemed, Offer: offer, ClientName: clientName, EntityName: entity.Name}, nil
}

// Confirm marks a redeemed claim as verified by the client. Only verified
// redemptions count as a popularity signal (Backend v2 §3.4, §6), so the signal
// is recorded here, once: a repeated confirmation changes nothing.
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

	// La condición va en el UPDATE: dos confirmaciones simultáneas no registran dos señales.
	res := s.ClaimRepo.DB.Model(&models.OfferClaim{}).
		Where("id = ? AND verified_by_client = ?", claim.ID, false).
		Update("verified_by_client", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 && s.SignalService != nil {
		s.SignalService.Record(claim.EntityID, models.SignalRedeem)
	}
	return nil
}
