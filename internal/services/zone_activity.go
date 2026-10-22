package services

import (
	"sort"
	"strings"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Mapa vivo (3.41, mockup mapa_vivo.html): lo que hace brillar una zona y lo que hace un pulso en el mapa. Todo sale
// de datos que Empre tiene: negocios registrados, ofertas vigentes, negocios nuevos y ofertas recién publicadas. Nada
// de ventas ni afluencia, que no se miden; ninguna zona va por encima de otra por pagar; sin conteos de ofertas.

const (
	ZoneGrowWindow   = 30 * 24 * time.Hour // «Nuevos»: negocios que llegaron en los últimos 30 días
	ZoneGrowMin      = 2                   // con uno solo no se dice que la zona «crece»
	FreshOfferWindow = 2 * time.Hour       // una oferta publicada hace menos de esto hace un pulso
	FreshOfferMax    = 10
)

// ActivityEntity es un negocio de la ciudad con lo que importa para su zona.
type ActivityEntity struct {
	ID        uuid.UUID
	ZoneID    *uuid.UUID
	City      string
	Latitude  float64
	Longitude float64
	CreatedAt time.Time
}

// ActivityOffer es una oferta vigente con cuándo se publicó.
type ActivityOffer struct {
	OfferID   uuid.UUID
	EntityID  uuid.UUID
	CreatedAt time.Time
}

type ZoneActivity struct {
	Zone       models.Zone
	Businesses int
	HasOffers  bool // hay al menos una oferta vigente (no cuántas)
	Growing    bool
}

type FreshOffer struct {
	OfferID     uuid.UUID
	EntityID    uuid.UUID
	Latitude    float64
	Longitude   float64
	PublishedAt time.Time
}

// BuildZoneActivity arma el brillo de cada zona de la ciudad y las ofertas que hacen pulso. Las zonas sin negocios no
// brillan (no se devuelven). Las ofertas frescas van de la más nueva a la más vieja, con tope.
func BuildZoneActivity(zones []models.Zone, city string, ents []ActivityEntity, offers []ActivityOffer, now time.Time) ([]ZoneActivity, []FreshOffer) {
	byID := map[uuid.UUID]ActivityEntity{}
	zoneInCity := map[uuid.UUID]bool{}
	for _, z := range zones {
		zoneInCity[z.ID] = SameCity(z.City, city)
	}
	// Un negocio es de la ciudad si su texto de ciudad coincide o si queda en una zona de esa ciudad (el texto puede
	// estar escrito de otra forma; la ubicación manda, como al asignar la zona).
	inCity := func(e ActivityEntity) bool {
		return strings.TrimSpace(city) == "" || SameCity(e.City, city) || (e.ZoneID != nil && zoneInCity[*e.ZoneID])
	}
	for _, e := range ents {
		if inCity(e) {
			byID[e.ID] = e
		}
	}
	type acc struct{ biz, fresh int }
	per := map[uuid.UUID]*acc{}
	for _, e := range byID {
		if e.ZoneID == nil {
			continue
		}
		a := per[*e.ZoneID]
		if a == nil {
			a = &acc{}
			per[*e.ZoneID] = a
		}
		a.biz++
		if now.Sub(e.CreatedAt) <= ZoneGrowWindow {
			a.fresh++
		}
	}
	withOffer := map[uuid.UUID]bool{}
	fresh := []FreshOffer{}
	for _, o := range offers {
		e, ok := byID[o.EntityID]
		if !ok {
			continue
		}
		if e.ZoneID != nil {
			withOffer[*e.ZoneID] = true
		}
		// Hasta 1 minuto «en el futuro» se acepta: relojes apenas desfasados entre la base y el servidor.
		if age := now.Sub(o.CreatedAt); age >= -time.Minute && age <= FreshOfferWindow {
			fresh = append(fresh, FreshOffer{OfferID: o.OfferID, EntityID: e.ID, Latitude: e.Latitude, Longitude: e.Longitude, PublishedAt: o.CreatedAt})
		}
	}
	out := []ZoneActivity{}
	for _, z := range zones {
		if !z.Active || (strings.TrimSpace(city) != "" && !SameCity(z.City, city)) {
			continue
		}
		a := per[z.ID]
		if a == nil || a.biz == 0 {
			continue
		}
		out = append(out, ZoneActivity{Zone: z, Businesses: a.biz, HasOffers: withOffer[z.ID], Growing: a.fresh >= ZoneGrowMin})
	}
	sort.SliceStable(out, func(i, j int) bool { return FoldText(out[i].Zone.Name) < FoldText(out[j].Zone.Name) })
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].PublishedAt.After(fresh[j].PublishedAt) })
	if len(fresh) > FreshOfferMax {
		fresh = fresh[:FreshOfferMax]
	}
	return out, fresh
}

type ZoneActivityService struct{ db *gorm.DB }

func NewZoneActivityService(db *gorm.DB) *ZoneActivityService { return &ZoneActivityService{db: db} }

// For devuelve la actividad de la ciudad: la de la ubicación o, sin ella, la que manda la app (`cityHint`).
func (s *ZoneActivityService) For(lat, long float64, hasLoc bool, cityHint string, now time.Time) (string, []ZoneActivity, []FreshOffer, error) {
	zones, err := NewZoneService(s.db).List("")
	if err != nil {
		return "", nil, nil, err
	}
	city, _ := ResolveCity(zones, nil, lat, long, hasLoc, cityHint)
	if city == "" {
		return "", []ZoneActivity{}, []FreshOffer{}, nil
	}
	var ents []ActivityEntity
	if err := s.db.Model(&models.Entity{}).Select("id", "zone_id", "city", "latitude", "longitude", "created_at").
		Where("LOWER(TRIM(city)) = LOWER(TRIM(?)) OR zone_id IN (SELECT id FROM zones WHERE LOWER(city) = LOWER(?))", city, city).
		Scan(&ents).Error; err != nil {
		return "", nil, nil, err
	}
	var offers []ActivityOffer
	if err := s.db.Model(&models.Offer{}).Select("id AS offer_id", "entity_id", "created_at").
		Where("ends_at > ? AND starts_at <= ? AND used_slots < total_slots", now, now).
		Scan(&offers).Error; err != nil {
		return "", nil, nil, err
	}
	act, fresh := BuildZoneActivity(zones, city, ents, offers, now)
	return city, act, fresh, nil
}
