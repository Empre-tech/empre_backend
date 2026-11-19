package services

import (
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

// Facetas de una oferta: cada dato en su campo, para que Ofertas las separe sin leer el título. Salen de reglas, no de
// un modelo: de lo que el dueño ya escribió y de los servicios del catálogo que incluyó.
const (
	BenefitMulti   = "multi"   // lleva más por menos: 2x1, 3x2
	BenefitFree    = "free"    // algo gratis
	BenefitPercent = "percent" // un porcentaje de descuento
	BenefitCombo   = "combo"   // varios servicios juntos
	BenefitPrice   = "price"   // un precio especial en pesos
	BenefitOther   = "other"

	OfferTagsMax = 8
)

var (
	multiRe   = regexp.MustCompile(`\b\d\s*x\s*\d\b`)
	percentRe = regexp.MustCompile(`\d\s*%`)
)

// OfferBenefit dice qué tipo de beneficio da la oferta, por lo que dicen su precio y su título.
func OfferBenefit(title, price string, special *int, items int) string {
	t := strings.ToLower(price + " " + title)
	switch {
	case multiRe.MatchString(t):
		return BenefitMulti
	case strings.Contains(t, "gratis") || (special != nil && *special == 0):
		return BenefitFree
	case percentRe.MatchString(t) || strings.Contains(t, "descuento"):
		return BenefitPercent
	case items >= 2:
		return BenefitCombo
	case special != nil || plainPrice.MatchString(price):
		return BenefitPrice
	}
	return BenefitOther
}

// Palabras que no describen lo ofertado: conectores y el vocabulario de cualquier promoción.
var offerTagStop = map[string]bool{
	"de": true, "del": true, "la": true, "el": true, "los": true, "las": true, "con": true, "sin": true, "y": true, "o": true,
	"para": true, "por": true, "en": true, "a": true, "al": true, "un": true, "una": true, "unos": true, "unas": true, "tu": true,
	"oferta": true, "ofertas": true, "promo": true, "promocion": true, "descuento": true, "gratis": true, "hoy": true,
	"solo": true, "mas": true, "precio": true, "especial": true, "lleva": true, "paga": true, "off": true, "dia": true, "semana": true,
}

// OfferTags son las palabras que describen lo ofertado (título y servicios incluidos), sin tildes, sin repetir y sin el
// vocabulario de promoción ni las cifras. Hasta OfferTagsMax, en el orden en que aparecen.
func OfferTags(title string, itemNames []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, text := range append([]string{title}, itemNames...) {
		for _, w := range strings.Fields(ledger.Normalize(text)) {
			if len(w) < 3 || offerTagStop[w] || seen[w] || strings.ContainsAny(w, "0123456789") {
				continue
			}
			seen[w] = true
			out = append(out, w)
			if len(out) == OfferTagsMax {
				return out
			}
		}
	}
	return out
}

var audienceOrder = []string{models.AudienceMen, models.AudienceWomen, models.AudienceKids, models.AudienceUnisex}

// OfferAudience une el público de los servicios incluidos, en un orden fijo. Vacío = no se dijo.
func OfferAudience(perItem [][]string) []string {
	has := map[string]bool{}
	for _, a := range perItem {
		for _, v := range a {
			has[v] = true
		}
	}
	out := []string{}
	for _, v := range audienceOrder {
		if has[v] {
			out = append(out, v)
		}
	}
	return out
}

// PickOfferCategory elige la categoría propia de la oferta. perItem trae, por cada servicio incluido, las categorías
// a las que pertenece su sección. Si la del negocio está entre ellas (o falta el dato de algún servicio), se queda la
// del negocio; si todos los servicios apuntan a una misma categoría distinta, gana esa. Ante la duda, la del negocio.
func PickOfferCategory(entityCat uuid.UUID, perItem [][]uuid.UUID) uuid.UUID {
	votes := map[uuid.UUID]int{}
	known := 0
	for _, cats := range perItem {
		if len(cats) == 0 {
			continue
		}
		known++
		for _, c := range cats {
			if c == entityCat {
				return entityCat
			}
			votes[c]++
		}
	}
	if known == 0 || known < len(perItem) {
		return entityCat
	}
	type kv struct {
		id uuid.UUID
		n  int
	}
	list := make([]kv, 0, len(votes))
	for id, n := range votes {
		list = append(list, kv{id, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].id.String() < list[j].id.String()
	})
	if list[0].n < known || (len(list) > 1 && list[1].n == list[0].n) {
		return entityCat
	}
	return list[0].id
}

// fillFacets calcula las facetas de una oferta con sus Items ya puestos. Nunca tumba la publicación: si algo falla al
// leer el catálogo, la categoría es la del negocio y el público queda sin decir.
func fillFacets(db *gorm.DB, o *models.Offer) {
	names := make([]string, 0, len(o.Items))
	ids := make([]uuid.UUID, 0, len(o.Items))
	for _, it := range o.Items {
		names = append(names, it.Name)
		if it.ServiceItemID != nil {
			ids = append(ids, *it.ServiceItemID)
		}
	}
	o.Benefit = OfferBenefit(o.Title, o.Price, o.SpecialPrice, len(o.Items))
	o.Tags = OfferTags(o.Title, names)

	var ent models.Entity
	if err := db.Select("id", "category_id").Where("id = ?", o.EntityID).First(&ent).Error; err != nil || ent.CategoryID == uuid.Nil {
		return
	}
	entityCat := ent.CategoryID
	o.CategoryID = &entityCat
	if len(ids) == 0 {
		return
	}
	var svcs []models.ServiceItem
	if err := db.Select("id", "section_id", "audience").Where("id IN ?", ids).Find(&svcs).Error; err != nil {
		return
	}
	audiences := make([][]string, 0, len(svcs))
	sections := []uuid.UUID{}
	bySvc := map[uuid.UUID]*uuid.UUID{}
	for _, s := range svcs {
		audiences = append(audiences, s.Audience)
		bySvc[s.ID] = s.SectionID
		if s.SectionID != nil {
			sections = append(sections, *s.SectionID)
		}
	}
	o.Audience = OfferAudience(audiences)

	bySection := map[uuid.UUID][]uuid.UUID{}
	if len(sections) > 0 {
		var rows []struct {
			SectionID  uuid.UUID
			CategoryID uuid.UUID
		}
		if err := db.Raw(`SELECT DISTINCT ss.section_id, sc.category_id FROM subcategory_sections ss
			JOIN subcategories sc ON sc.id = ss.subcategory_id AND sc.deleted_at IS NULL
			WHERE ss.section_id IN ?`, sections).Scan(&rows).Error; err != nil {
			return
		}
		for _, r := range rows {
			bySection[r.SectionID] = append(bySection[r.SectionID], r.CategoryID)
		}
	}
	perItem := make([][]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if sec := bySvc[id]; sec != nil {
			perItem = append(perItem, bySection[*sec])
		} else {
			perItem = append(perItem, nil)
		}
	}
	cat := PickOfferCategory(entityCat, perItem)
	o.CategoryID = &cat
}

// BackfillOfferFacets pone las facetas a las ofertas publicadas antes de que existieran. Se puede repetir: solo toca
// las que aún no tienen tipo de beneficio.
func (s *OfferService) BackfillOfferFacets() (int, error) {
	var offers []models.Offer
	if err := s.OfferRepo.DB.Preload("Items").Where("benefit = ''").Find(&offers).Error; err != nil {
		return 0, err
	}
	for i := range offers {
		o := &offers[i]
		fillFacets(s.OfferRepo.DB, o)
		if err := s.OfferRepo.DB.Model(&models.Offer{}).Where("id = ?", o.ID).
			Select("category_id", "benefit", "audience", "tags").
			Updates(&models.Offer{CategoryID: o.CategoryID, Benefit: o.Benefit, Audience: o.Audience, Tags: o.Tags}).Error; err != nil {
			return i, err
		}
	}
	return len(offers), nil
}
