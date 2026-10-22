package services

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// Cómo le fue a cada oferta de un negocio y qué dicen sus ofertas pasadas (solo para el dueño). Las sugerencias
// salen de reglas fijas y probadas, solo con la historia del propio negocio y con datos suficientes; la redacción
// va en pasado y no promete un efecto.
const (
	// StatsWindow: qué ofertas entran (las que empezaron en los últimos 90 días).
	StatsWindow = 90 * 24 * time.Hour
	// StatsMaxOffers: tope de ofertas que se analizan.
	StatsMaxOffers = 60
	// Mínimos para decir algo: ofertas terminadas, vistas en total y ofertas por lado en cada comparación.
	InsightMinOffers = 3
	InsightMinViews  = 20
	InsightMinSide   = 2
	// ShortOfferMinutes: hasta cuánto dura una oferta «corta» (2 h).
	ShortOfferMinutes = 120
)

// OfferStat es una oferta con sus resultados.
type OfferStat struct {
	OfferID      uuid.UUID
	Title        string
	Type         models.OfferType
	StartsAt     time.Time
	EndsAt       time.Time
	TotalSlots   int
	SpecialPrice *int
	Ended        bool
	// Views: instalaciones distintas que vieron la oferta (evento anónimo offer_viewed).
	Views int64
	// Claims: reclamos hechos (en cualquier estado); Redeemed: los canjeados.
	Claims   int64
	Redeemed int64
	// FilledMinutes: minutos desde el inicio hasta que se reclamó el último cupo; nil si no se agotó.
	FilledMinutes *int
	// Hour y Weekday del inicio, en hora de Colombia; DurationMinutes, cuánto duró (o dura).
	Hour            int
	Weekday         int
	DurationMinutes int
}

// OfferInsight es un hallazgo en palabras para el dueño.
type OfferInsight struct {
	Kind string // band | duration | fill
	Text string
}

// OfferStatsSummary resume las ofertas terminadas y dice qué se puede sugerir.
type OfferStatsSummary struct {
	Finished int
	Views    int64
	Claims   int64
	Redeemed int64
	// Enough: hay datos suficientes para sugerir (InsightMinOffers terminadas y InsightMinViews vistas).
	Enough   bool
	Insights []OfferInsight
	// BestOfferID: la terminada con más canjes (a igualdad, la que mejor convirtió reclamos en canjes).
	BestOfferID *uuid.UUID
	// PreferBand: la franja que mejor funcionó («mañana», «tarde», «noche»); vacía si no hay una clara.
	PreferBand string
	// PreferShort: las de 2 h o menos agotaron cupos más veces que las largas.
	PreferShort bool
	// AvgFillMinutes: cuánto tardaron en agotarse, en promedio, las que se agotaron.
	AvgFillMinutes *int
}

// Band es la franja del día de una hora (0 a 23).
func Band(hour int) string {
	switch {
	case hour < 12:
		return "mañana"
	case hour < 19:
		return "tarde"
	default:
		return "noche"
	}
}

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// BuildOfferSummary arma el resumen y los hallazgos con las ofertas terminadas. Es pura: se prueba sin base de datos.
func BuildOfferSummary(stats []OfferStat) OfferStatsSummary {
	var done []OfferStat
	for _, s := range stats {
		if s.Ended {
			done = append(done, s)
		}
	}
	sum := OfferStatsSummary{Finished: len(done), Insights: []OfferInsight{}}
	for _, s := range done {
		sum.Views += s.Views
		sum.Claims += s.Claims
		sum.Redeemed += s.Redeemed
	}
	sum.Enough = len(done) >= InsightMinOffers && sum.Views >= InsightMinViews
	if !sum.Enough {
		return sum
	}

	// Mejor oferta: más canjes; a igualdad, mejor tasa de canje; a igualdad, la más reciente.
	best := append([]OfferStat(nil), done...)
	sort.SliceStable(best, func(i, j int) bool {
		a, b := best[i], best[j]
		if a.Redeemed != b.Redeemed {
			return a.Redeemed > b.Redeemed
		}
		ra, rb := ratio(float64(a.Redeemed), float64(a.Claims)), ratio(float64(b.Redeemed), float64(b.Claims))
		if ra != rb {
			return ra > rb
		}
		return a.StartsAt.After(b.StartsAt)
	})
	if best[0].Redeemed > 0 {
		id := best[0].OfferID
		sum.BestOfferID = &id
	}

	// Franja: solo las «por horas» (una de todo el día no tiene franja); canjes promedio por oferta.
	type agg struct{ n, red int64 }
	bands := map[string]*agg{}
	for _, s := range done {
		if s.Type != models.OfferHours {
			continue
		}
		b := Band(s.Hour)
		if bands[b] == nil {
			bands[b] = &agg{}
		}
		bands[b].n++
		bands[b].red += s.Redeemed
	}
	type bandAvg struct {
		name string
		avg  float64
	}
	var avgs []bandAvg
	for name, a := range bands {
		if a.n >= InsightMinSide {
			avgs = append(avgs, bandAvg{name, float64(a.red) / float64(a.n)})
		}
	}
	sort.Slice(avgs, func(i, j int) bool {
		if avgs[i].avg != avgs[j].avg {
			return avgs[i].avg > avgs[j].avg
		}
		return avgs[i].name < avgs[j].name
	})
	if len(avgs) >= 2 {
		top, low := avgs[0], avgs[len(avgs)-1]
		if top.avg > 0 && top.avg >= low.avg*1.5 {
			times := "más"
			if low.avg > 0 {
				times = strings.Replace(fmt.Sprintf("%.1f veces más", top.avg/low.avg), ".", ",", 1)
			}
			sum.PreferBand = top.name
			sum.Insights = append(sum.Insights, OfferInsight{Kind: "band", Text: fmt.Sprintf("En tus ofertas pasadas, las de la %s tuvieron %s canjes que las de la %s.", top.name, times, low.name)})
		}
	}

	// Duración: de las «por horas», qué parte agotó los cupos según dure 2 h o menos, o más.
	var shortN, shortFull, longN, longFull int
	for _, s := range done {
		if s.Type != models.OfferHours {
			continue
		}
		full := s.FilledMinutes != nil
		if s.DurationMinutes <= ShortOfferMinutes {
			shortN++
			if full {
				shortFull++
			}
		} else {
			longN++
			if full {
				longFull++
			}
		}
	}
	if shortN >= InsightMinSide && longN >= InsightMinSide && ratio(float64(shortFull), float64(shortN)) > ratio(float64(longFull), float64(longN)) {
		sum.PreferShort = true
		sum.Insights = append(sum.Insights, OfferInsight{Kind: "duration", Text: fmt.Sprintf("Las de 2 h o menos agotaron los cupos %d de %d veces; las más largas, %d de %d.", shortFull, shortN, longFull, longN)})
	}

	// Cuánto tardaron en agotarse las que se agotaron.
	var fills []int
	for _, s := range done {
		if s.FilledMinutes != nil {
			fills = append(fills, *s.FilledMinutes)
		}
	}
	if len(fills) >= InsightMinSide {
		total := 0
		for _, f := range fills {
			total += f
		}
		avg := total / len(fills)
		sum.AvgFillMinutes = &avg
		sum.Insights = append(sum.Insights, OfferInsight{Kind: "fill", Text: fmt.Sprintf("Cuando se agotaron, fue en unos %d min en promedio: podrías ofrecer algunos cupos más.", avg)})
	}
	return sum
}

// Stats devuelve, para el dueño, cada oferta de los últimos 90 días con sus resultados (la más reciente primero) y el
// resumen con los hallazgos.
func (s *OfferService) Stats(entityID, ownerID uuid.UUID, now time.Time) ([]OfferStat, OfferStatsSummary, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, OfferStatsSummary{}, err
	}
	db := s.OfferRepo.DB
	var offers []models.Offer
	if err := db.Where("entity_id = ? AND starts_at >= ?", entityID, now.Add(-StatsWindow)).
		Order("starts_at DESC").Limit(StatsMaxOffers).Find(&offers).Error; err != nil {
		return nil, OfferStatsSummary{}, err
	}
	if len(offers) == 0 {
		return []OfferStat{}, BuildOfferSummary(nil), nil
	}
	ids := make([]uuid.UUID, 0, len(offers))
	idText := make([]string, 0, len(offers))
	for _, o := range offers {
		ids = append(ids, o.ID)
		idText = append(idText, o.ID.String())
	}

	type viewRow struct {
		OfferID string
		N       int64
	}
	var views []viewRow
	if err := db.Raw(`SELECT props->>'offer_id' AS offer_id, COUNT(DISTINCT anon_id) AS n FROM events
		WHERE name = 'offer_viewed' AND entity_id = ? AND props->>'offer_id' IN ? GROUP BY 1`, entityID, idText).Scan(&views).Error; err != nil {
		return nil, OfferStatsSummary{}, err
	}
	viewsOf := map[string]int64{}
	for _, v := range views {
		viewsOf[v.OfferID] = v.N
	}

	type claimRow struct {
		OfferID  uuid.UUID
		Claims   int64
		Redeemed int64
	}
	var claims []claimRow
	if err := db.Model(&models.OfferClaim{}).
		Select("offer_id, COUNT(*) AS claims, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS redeemed", models.ClaimRedeemed).
		Where("offer_id IN ?", ids).Group("offer_id").Scan(&claims).Error; err != nil {
		return nil, OfferStatsSummary{}, err
	}
	claimsOf := map[uuid.UUID]claimRow{}
	for _, c := range claims {
		claimsOf[c.OfferID] = c
	}

	loc := utils.NowInCartagena().Location()
	out := make([]OfferStat, 0, len(offers))
	for _, o := range offers {
		c := claimsOf[o.ID]
		st := OfferStat{
			OfferID:         o.ID,
			Title:           o.Title,
			Type:            o.Type,
			StartsAt:        o.StartsAt,
			EndsAt:          o.EndsAt,
			TotalSlots:      o.TotalSlots,
			SpecialPrice:    o.SpecialPrice,
			Ended:           !o.EndsAt.After(now),
			Views:           viewsOf[o.ID.String()],
			Claims:          c.Claims,
			Redeemed:        c.Redeemed,
			Hour:            o.StartsAt.In(loc).Hour(),
			Weekday:         int(o.StartsAt.In(loc).Weekday()),
			DurationMinutes: int(o.EndsAt.Sub(o.StartsAt).Minutes()),
		}
		// Se agotó si llegó a tantos reclamos como cupos: el momento es el del último cupo reclamado.
		if o.TotalSlots > 0 && c.Claims >= int64(o.TotalSlots) {
			var at time.Time
			if err := db.Model(&models.OfferClaim{}).Select("claimed_at").Where("offer_id = ?", o.ID).
				Order("claimed_at ASC").Offset(o.TotalSlots - 1).Limit(1).Scan(&at).Error; err == nil && !at.IsZero() {
				m := int(at.Sub(o.StartsAt).Minutes())
				if m < 0 {
					m = 0
				}
				st.FilledMinutes = &m
			}
		}
		out = append(out, st)
	}
	return out, BuildOfferSummary(out), nil
}
