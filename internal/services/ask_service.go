package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// «Pregúntale a Empre»: el cliente escribe o dice lo que necesita («dónde almuerzo barato cerca») y la respuesta son
// negocios reales cerca. La IA solo entiende la pregunta (qué busca y con qué condiciones); los negocios, precios y
// horarios salen siempre de la base de datos, y el texto de la respuesta lo arma el servidor con esos datos, así la IA
// no puede inventar un negocio ni un precio. Sin IA configurada funciona igual con las palabras de la pregunta.

const (
	askMaxQuestion = 200
	askWalkMPerMin = 80.0 // a pie: ~80 m por minuto
	askMaxResults  = 12
)

// askRadii: primero cerca; si no hay nada, se amplía.
var askRadii = []float64{2500, 6000, 15000}

type AskService struct {
	DB     *gorm.DB
	Search *SearchService
	AI     *AIService
}

func NewAskService(db *gorm.DB, search *SearchService, ai *AIService) *AskService {
	return &AskService{DB: db, Search: search, AI: ai}
}

// AskFilters son las condiciones que se apilan («que esté abierto», «con oferta», «a domicilio»).
type AskFilters struct {
	OpenNow   bool `json:"open_now"`
	WithOffer bool `json:"with_offer"`
	Delivery  bool `json:"delivery"`
	Cheapest  bool `json:"cheapest"`
}

type AskInput struct {
	Question string
	Lat, Lng float64
	Filters  AskFilters // las que el cliente activó con un toque; se suman a las que entienda la IA
}

type AskItem struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Price      *int      `json:"price"`
	PriceFrom  bool      `json:"price_from"`
	Photo      string    `json:"photo,omitempty"`
	Section    string    `json:"section,omitempty"`
	EntityID   uuid.UUID `json:"entity_id"`
	EntityName string    `json:"entity_name"`
	Minutes    int       `json:"minutes"`
}

type AskBusiness struct {
	dtos.SearchBusiness
	Minutes  int  `json:"minutes"`
	Delivery bool `json:"delivery"`
	// Verificado por Empre: la app lo usa en el filtro «Verificado» del buscador (3.66).
	IsVerified bool     `json:"is_verified"`
	Item       *AskItem `json:"item,omitempty"` // el producto que coincide (el más barato si se pidió barato)
}

type AskResponse struct {
	Reply       string        `json:"reply"`
	Terms       string        `json:"terms"`
	Filters     AskFilters    `json:"filters"`
	Businesses  []AskBusiness `json:"businesses"`
	Items       []AskItem     `json:"items"`
	Widened     bool          `json:"widened"` // no había cerca: se buscó más lejos
	Suggestions []string      `json:"suggestions"`
	UsedAI      bool          `json:"used_ai"`
}

// askIntentTool: lo único que se le pide a la IA.
var askIntentTool = AITool{
	Name:        "buscar",
	Description: "Convierte la pregunta del cliente en una búsqueda de negocios cerca.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{
"terms":{"type":"string","description":"Qué busca, en 1 a 3 palabras en singular y en español, como lo vendería un negocio: «almuerzo», «barbería», «droguería», «jean», «arreglo de motos». Sin palabras de condición (cerca, barato, abierto)."},
"open_now":{"type":"boolean","description":"Pide que esté abierto ahora."},
"with_offer":{"type":"boolean","description":"Pide algo en oferta o con descuento."},
"delivery":{"type":"boolean","description":"Pide domicilio o que se lo lleven."},
"cheapest":{"type":"boolean","description":"Pide lo más barato o económico."}
},"required":["terms"]}`),
}

const askSystemPrompt = "Eres el buscador de Empre, una app de negocios de barrio en Cartagena (Colombia). " +
	"Recibes lo que una persona necesita y llamas a la herramienta «buscar». Nunca respondes con texto."

// Ask responde la pregunta con negocios reales cerca.
func (s *AskService) Ask(in AskInput) (*AskResponse, error) {
	q := strings.TrimSpace(in.Question)
	if q == "" {
		return nil, fmt.Errorf("%w: escribe qué necesitas", ErrCatalogInvalid)
	}
	if utf8.RuneCountInString(q) > askMaxQuestion {
		q = string([]rune(q)[:askMaxQuestion])
	}
	res := &AskResponse{Businesses: []AskBusiness{}, Items: []AskItem{}, Suggestions: []string{}, Filters: in.Filters}
	terms, f, usedAI := s.intent(q)
	res.UsedAI = usedAI
	res.Terms = terms
	res.Filters = AskFilters{
		OpenNow:   in.Filters.OpenNow || f.OpenNow,
		WithOffer: in.Filters.WithOffer || f.WithOffer,
		Delivery:  in.Filters.Delivery || f.Delivery,
		Cheapest:  in.Filters.Cheapest || f.Cheapest,
	}
	if in.Lat == 0 && in.Lng == 0 {
		in.Lat, in.Lng = cartagenaLat, cartagenaLng
	}
	for i, radius := range askRadii {
		bizs, items := s.find(terms, in.Lat, in.Lng, radius, res.Filters)
		if len(bizs) > 0 || i == len(askRadii)-1 {
			res.Businesses, res.Items = bizs, items
			res.Widened = i > 0 && len(bizs) > 0
			break
		}
	}
	// Listas vacías (no null) en el JSON: la app las recorre directo.
	if res.Businesses == nil {
		res.Businesses = []AskBusiness{}
	}
	if res.Items == nil {
		res.Items = []AskItem{}
	}
	res.Reply = askReply(terms, res)
	res.Suggestions = askSuggestions(res)
	return res, nil
}

// Centro de Cartagena: si el teléfono no da ubicación.
const (
	cartagenaLat = 10.4236
	cartagenaLng = -75.5478
)

// intent: con IA, qué busca y con qué condiciones; sin IA (o si falla), las palabras de la pregunta y las condiciones
// que se reconocen por palabras clave.
func (s *AskService) intent(q string) (string, AskFilters, bool) {
	f := keywordFilters(q)
	if s.AI != nil && s.AI.Enabled() {
		reply, err := s.AI.ChatTool(askSystemPrompt, []AIMessage{{Role: AIRoleUser, Content: q}}, askIntentTool)
		if err == nil {
			for _, call := range reply.ToolCalls {
				var in struct {
					Terms     string `json:"terms"`
					OpenNow   bool   `json:"open_now"`
					WithOffer bool   `json:"with_offer"`
					Delivery  bool   `json:"delivery"`
					Cheapest  bool   `json:"cheapest"`
				}
				if json.Unmarshal(call.Input, &in) == nil && strings.TrimSpace(in.Terms) != "" {
					return cleanTerms(in.Terms), AskFilters{
						OpenNow: in.OpenNow || f.OpenNow, WithOffer: in.WithOffer || f.WithOffer,
						Delivery: in.Delivery || f.Delivery, Cheapest: in.Cheapest || f.Cheapest,
					}, true
				}
			}
		}
	}
	return cleanTerms(q), f, false
}

var askStopwords = map[string]bool{
	"donde": true, "dónde": true, "hay": true, "un": true, "una": true, "unos": true, "unas": true, "el": true, "la": true,
	"los": true, "las": true, "de": true, "del": true, "en": true, "para": true, "por": true, "que": true, "qué": true,
	"me": true, "mi": true, "quiero": true, "necesito": true, "busco": true, "algo": true, "cerca": true, "cercano": true,
	"cercana": true, "aqui": true, "aquí": true, "barato": true, "barata": true, "economico": true, "económico": true,
	"abierto": true, "abierta": true, "ahora": true, "ahorita": true, "hoy": true, "oferta": true, "ofertas": true,
	"domicilio": true, "y": true, "o": true, "con": true, "sin": true, "puedo": true, "a": true, "al": true, "lo": true,
	"mas": true, "más": true, "bueno": true, "buena": true, "rico": true, "rica": true, "tienen": true, "venden": true,
	"cortarme": true, "arreglar": true, "comprar": true, "almorzar": false,
}

// cleanTerms deja las palabras que dicen QUÉ se busca (máx. 3).
func cleanTerms(q string) string {
	q = strings.ToLower(strings.Trim(strings.TrimSpace(q), "¿?¡!.,"))
	out := []string{}
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return r == ' ' || r == ',' || r == '?' || r == '¿' || r == '.' }) {
		if askStopwords[w] || utf8.RuneCountInString(w) < 3 {
			continue
		}
		if w == "almorzar" {
			w = "almuerzo"
		}
		out = append(out, w)
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return q
	}
	return strings.Join(out, " ")
}

func keywordFilters(q string) AskFilters {
	l := strings.ToLower(q)
	has := func(ws ...string) bool {
		for _, w := range ws {
			if strings.Contains(l, w) {
				return true
			}
		}
		return false
	}
	return AskFilters{
		OpenNow:   has("abierto", "abierta", "ahora", "ahorita"),
		WithOffer: has("oferta", "descuento", "promo"),
		Delivery:  has("domicilio", "me lo lleven", "a la casa"),
		Cheapest:  has("barat", "económic", "economic"),
	}
}

// find: negocios que coinciden (por categoría, nombre, descripción o por lo que venden) dentro del radio.
func (s *AskService) find(terms string, lat, lng, radius float64, f AskFilters) ([]AskBusiness, []AskItem) {
	minLat, maxLat, minLng, maxLng := utils.BBoxAround(lat, lng, radius)
	words := strings.Fields(terms)
	found := map[uuid.UUID]models.Entity{}
	add := func(es []models.Entity) {
		for _, e := range es {
			found[e.ID] = e
		}
	}
	repo := s.Search.Repo
	for _, w := range append([]string{terms}, words...) {
		if cat, err := repo.MatchCategory(w); err == nil && cat != nil {
			es, _ := repo.FindByCategoryInBBox(cat.ID, minLat, maxLat, minLng, maxLng, 40)
			add(es)
		}
		es, _ := repo.FindByTextInBBox(w, minLat, maxLat, minLng, maxLng, 40)
		add(es)
		if len(words) == 1 {
			break
		}
	}
	// Lo que venden: productos y servicios activos que coinciden, en negocios dentro del radio.
	items := s.matchingItems(words, minLat, maxLat, minLng, maxLng)
	missing := []uuid.UUID{}
	for _, it := range items {
		if _, ok := found[it.EntityID]; !ok {
			missing = append(missing, it.EntityID)
		}
	}
	if len(missing) > 0 {
		var es []models.Entity
		s.DB.Joins("Category").Preload("Hours").Where("entities.id IN ?", missing).Find(&es)
		add(es)
	}
	if len(found) == 0 {
		return nil, nil
	}
	list := make([]models.Entity, 0, len(found))
	for _, e := range found {
		list = append(list, e)
	}
	base := s.Search.toBusinesses(list, lat, lng)
	byEntity := map[uuid.UUID]*AskItem{}
	for i := range items {
		it := &items[i]
		if e, ok := found[it.EntityID]; ok {
			it.EntityName = e.Name
			it.Minutes = walkMinutes(utils.DistanceMeters(lat, lng, e.Latitude, e.Longitude))
		}
		cur := byEntity[it.EntityID]
		if cur == nil || cheaper(it, cur) {
			byEntity[it.EntityID] = it
		}
	}
	out := make([]AskBusiness, 0, len(base))
	for i, b := range base {
		e := found[b.ID]
		ab := AskBusiness{SearchBusiness: base[i], Minutes: walkMinutes(b.DistanceKM * 1000),
			Delivery: e.ServiceMode == models.ServiceModeDelivery || e.ServiceMode == models.ServiceModeBoth, IsVerified: e.IsVerified, Item: byEntity[b.ID]}
		if (f.OpenNow && b.HasHours && !b.IsOpenNow) || (f.WithOffer && !b.HasActiveOffer) || (f.Delivery && !ab.Delivery) {
			continue
		}
		out = append(out, ab)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if f.Cheapest && (out[i].Item != nil) != (out[j].Item != nil) {
			return out[i].Item != nil
		}
		if f.Cheapest && out[i].Item != nil && out[j].Item != nil && priceOf(out[i].Item) != priceOf(out[j].Item) {
			return priceOf(out[i].Item) < priceOf(out[j].Item)
		}
		if out[i].IsOpenNow != out[j].IsOpenNow {
			return out[i].IsOpenNow
		}
		return out[i].DistanceKM < out[j].DistanceKM
	})
	if len(out) > askMaxResults {
		out = out[:askMaxResults]
	}
	kept := map[uuid.UUID]bool{}
	for _, b := range out {
		kept[b.ID] = true
	}
	shown := []AskItem{}
	for _, it := range items {
		if kept[it.EntityID] {
			shown = append(shown, it)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		if f.Cheapest {
			return priceOf(&shown[i]) < priceOf(&shown[j])
		}
		return shown[i].Minutes < shown[j].Minutes
	})
	if len(shown) > 6 {
		shown = shown[:6]
	}
	return out, shown
}

func (s *AskService) matchingItems(words []string, minLat, maxLat, minLng, maxLng float64) []AskItem {
	if len(words) == 0 {
		return nil
	}
	cond := []string{}
	args := []any{}
	for _, w := range words {
		if utf8.RuneCountInString(w) < 3 {
			continue
		}
		like := "%" + w + "%"
		cond = append(cond, "unaccent(lower(service_items.name)) LIKE unaccent(lower(?)) OR unaccent(lower(service_items.section)) LIKE unaccent(lower(?))")
		args = append(args, like, like)
	}
	if len(cond) == 0 {
		return nil
	}
	var rows []models.ServiceItem
	s.DB.Joins("JOIN entities ON entities.id = service_items.entity_id").
		Where("service_items.status = ? AND service_items.parent_id IS NULL AND entities.deleted_at IS NULL", models.ServiceActive).
		Where("entities.latitude BETWEEN ? AND ? AND entities.longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Where("("+strings.Join(cond, " OR ")+")", args...).
		Limit(60).Find(&rows)
	out := make([]AskItem, 0, len(rows))
	for _, r := range rows {
		it := AskItem{ID: r.ID, Name: r.Name, Price: r.Price, PriceFrom: r.PriceFrom, Section: r.Section, EntityID: r.EntityID}
		if len(r.Photos) > 0 {
			it.Photo = r.Photos[0]
		}
		out = append(out, it)
	}
	return out
}

func walkMinutes(meters float64) int {
	m := int(math.Ceil(meters / askWalkMPerMin))
	if m < 1 {
		m = 1
	}
	return m
}

func priceOf(it *AskItem) int {
	if it == nil || it.Price == nil {
		return math.MaxInt32
	}
	return *it.Price
}

func cheaper(a, b *AskItem) bool { return priceOf(a) < priceOf(b) }

// askReply arma el texto con los datos reales (nunca con lo que diga la IA).
func askReply(terms string, r *AskResponse) string {
	n := len(r.Businesses)
	if n == 0 {
		return fmt.Sprintf("No encontré «%s» en Empre por ahora. Prueba con otras palabras o mira las categorías.", terms)
	}
	open := 0
	far := 0
	for _, b := range r.Businesses {
		if b.IsOpenNow {
			open++
		}
		if b.Minutes > far {
			far = b.Minutes
		}
	}
	lugar := "lugares"
	if n == 1 {
		lugar = "lugar"
	}
	head := fmt.Sprintf("Encontré %d %s", n, lugar)
	if r.Filters.OpenNow || open == n {
		head = fmt.Sprintf("Hay %d %s abiertos", n, lugar)
		if n == 1 {
			head = "Hay 1 lugar abierto"
		}
	}
	if far <= 15 {
		head += fmt.Sprintf(" a menos de %d minutos a pie", far)
	} else if r.Widened {
		head += " un poco más lejos de lo normal"
	}
	head += "."
	first := r.Businesses[0]
	if r.Filters.Cheapest && first.Item != nil && first.Item.Price != nil {
		return head + fmt.Sprintf(" El más barato: %s en %s, a %s.", first.Item.Name, first.Name, pesos(*first.Item.Price))
	}
	if first.Item != nil {
		return head + fmt.Sprintf(" El más cerca: %s, con %s (%d min).", first.Name, first.Item.Name, first.Minutes)
	}
	return head + fmt.Sprintf(" El más cerca: %s (%d min).", first.Name, first.Minutes)
}

func askSuggestions(r *AskResponse) []string {
	out := []string{}
	if len(r.Businesses) == 0 {
		return []string{"Ver categorías"}
	}
	out = append(out, "Verlos en el mapa")
	if !r.Filters.OpenNow {
		out = append(out, "Solo abiertos")
	}
	if !r.Filters.WithOffer {
		out = append(out, "Solo con oferta")
	}
	if !r.Filters.Delivery {
		out = append(out, "Que tenga domicilio")
	}
	if !r.Filters.Cheapest {
		out = append(out, "El más barato")
	}
	return out
}
