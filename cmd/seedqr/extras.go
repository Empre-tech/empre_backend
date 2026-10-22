package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/seedphotos"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type postSpec struct {
	caption  string
	agoHours int
	photos   int
}

type reviewSpec struct {
	who     int // índice del reseñador de ejemplo
	rating  int
	comment string
	agoDays int
}

type offerSpec struct {
	offerType  models.OfferType
	title      string
	conditions string
	price      string
	lasts      time.Duration
	slots      int
}

// catSpec es un servicio más del catálogo (aparte de los que entran en la oferta), con su sección.
type catSpec struct {
	name, desc, section string
	price               int // 0 = gratis; -1 = sin precio («Consultar»)
	from                bool
}

type pastOffer struct {
	title     string
	daysAgo   int
	slots     int
	used      int
	duration  time.Duration
	offerType models.OfferType
}

// extra es el contenido de ejemplo de cada negocio, en el mismo orden que `shops`: fotos (por publicación), reseñas,
// una segunda oferta vigente y ofertas ya terminadas (para «Cómo te va» del dueño).
type extra struct {
	top, bottom color.RGBA
	motif       seedphotos.Motif
	posts       []postSpec
	reviews     []reviewSpec
	second      *offerSpec
	past        []pastOffer
	// Catálogo de ejemplo con secciones: la app lo llama Carta, Servicios, Productos o Reparaciones según la categoría.
	catalog []catSpec
}

var reviewerNames = []string{"Laura Gómez", "Andrés Pertuz", "Camila Rojas", "Diego Fontalvo"}

var extras = []extra{
	{ // La Sazón de Rosa: muchas fotos (se ve «+N»), dos ofertas vigentes (se apilan en el perfil) y buenas reseñas.
		top: color.RGBA{245, 154, 75, 255}, bottom: color.RGBA{142, 59, 0, 255}, motif: seedphotos.MotifPlate,
		posts: []postSpec{
			{"Hoy: sancocho de pescado con arroz de coco y patacón", 3, 2},
			{"Jugos naturales de corozo y lulo, recién hechos", 20, 1},
			{"Así quedó el comedor después de la remodelación", 50, 3},
			{"Los viernes, cazuela de mariscos", 120, 2},
		},
		reviews: []reviewSpec{
			{0, 5, "El sancocho sabe a casa. Atención muy amable y rápida.", 2},
			{1, 5, "Porciones generosas y buen precio. Volveré el viernes por la cazuela.", 6},
			{2, 4, "Muy rico todo; a la hora del almuerzo hay que esperar un poco.", 12},
		},
		second: &offerSpec{models.OfferDay, "Postre de la casa gratis con tu almuerzo", "Un postre por persona, solo en el local.", "Gratis", 7 * time.Hour, 15},
		past: []pastOffer{
			{"Almuerzo ejecutivo 2x1", 3, 10, 9, 3 * time.Hour, models.OfferHours},
			{"Jugo gratis con el almuerzo", 6, 12, 7, 2 * time.Hour, models.OfferHours},
			{"Bandeja de mariscos con 20 %", 10, 8, 8, 24 * time.Hour, models.OfferDay},
		},
		catalog: []catSpec{
			{"Corrientazo", "Sopa y seco", "Almuerzos", 14000, false}, {"Almuerzo infantil", "Porción pequeña", "Almuerzos", 15000, false},
			{"Bandeja de mariscos", "Para compartir", "Platos fuertes", 58000, false}, {"Cazuela de mariscos", "Solo viernes", "Platos fuertes", 42000, false},
			{"Pescado frito", "Con patacón y arroz de coco", "Platos fuertes", 35000, false}, {"Arroz de coco", "Porción", "Acompañantes", 8000, false},
			{"Patacón", "Porción", "Acompañantes", 6000, false}, {"Jugo de corozo", "Natural", "Bebidas", 6000, false},
			{"Limonada de coco", "Natural", "Bebidas", 9000, false}, {"Tinto", "Café de la Sierra", "Bebidas", 2500, false},
			{"Enyucado", "Tradicional", "Postres", 5000, false}, {"Domicilio", "Hasta 3 km", "Otros", -1, false},
		},
	},
	{ // Barbería El Parque: fotos y reseñas; ofertas terminadas para ver los resultados.
		top: color.RGBA{43, 163, 160, 255}, bottom: color.RGBA{4, 58, 60, 255}, motif: seedphotos.MotifPole,
		posts: []postSpec{
			{"Degradado con diseño: el corte de la semana", 6, 2},
			{"Nuevo sillón y más espacio para esperar", 30, 1},
			{"Arreglo de barba con toalla caliente", 72, 1},
		},
		reviews: []reviewSpec{
			{1, 5, "El mejor degradado de la zona. Puntuales con la cita.", 1},
			{3, 4, "Buen corte y buen ambiente; los sábados se llena.", 9},
			{0, 5, "Me arreglaron la barba como pedí. Recomendado.", 15},
			{2, 4, "Precio justo y trato amable.", 20},
		},
		past: []pastOffer{
			{"Corte + barba con 25 %", 2, 6, 6, 8 * time.Hour, models.OfferDay},
			{"Corte infantil a mitad de precio", 5, 8, 3, 3 * time.Hour, models.OfferHours},
			{"Arreglo de barba gratis con el corte", 9, 6, 5, 2 * time.Hour, models.OfferHours},
		},
		catalog: []catSpec{
			{"Degradado", "40 min", "Cortes", 28000, false}, {"Diseño en el corte", "Líneas o figuras", "Cortes", 8000, true},
			{"Afeitado clásico", "Con navaja", "Barba", 15000, false}, {"Cejas", "Perfilado", "Barba", 6000, false},
			{"Tinte de barba", "", "Color", 20000, true}, {"Corte infantil", "Hasta 12 años", "Niños", 18000, false},
		},
	},
	{ // Café Sierra Nevada: dos fotos y dos reseñas.
		top: color.RGBA{200, 162, 122, 255}, bottom: color.RGBA{60, 32, 14, 255}, motif: seedphotos.MotifCup,
		posts: []postSpec{
			{"Pandebono recién salido del horno, todas las mañanas", 2, 1},
			{"Nuestro café de la Sierra, tostado aquí mismo", 40, 1},
		},
		catalog: []catSpec{
			{"Tinto", "Café de la Sierra", "Café", 2500, false}, {"Capuchino", "", "Café", 6000, false}, {"Pandebono", "Recién horneado", "Panadería", 2000, false},
			{"Torta de queso", "Porción", "Postres", 7000, false},
		},
		reviews: []reviewSpec{
			{2, 5, "El tinto más rico del barrio y el pandebono calientico.", 3},
			{3, 4, "Buen café; el sitio es pequeño pero acogedor.", 8},
		},
	},
	{ // Veterinaria Patitas: una sola foto (ocupa todo el ancho) y una reseña (aún sin nota: hacen falta 3).
		top: color.RGBA{91, 192, 190, 255}, bottom: color.RGBA{10, 58, 58, 255}, motif: seedphotos.MotifPaw,
		posts: []postSpec{
			{"Jornada de vacunación este sábado", 26, 1},
		},
		reviews: []reviewSpec{
			{0, 5, "Atendieron a mi perrita con mucho cariño.", 4},
		},
		catalog: []catSpec{
			{"Consulta general", "30 min", "Consultas", 45000, false}, {"Vacuna triple", "Perros", "Vacunas", 60000, false},
			{"Baño y corte", "Según el tamaño", "Estética", 40000, true}, {"Urgencias", "Las 24 horas", "Urgencias", -1, false},
		},
	},
	{ // CeluFix Express: sin fotos ni reseñas, para ver «Aún sin fotos» y «Aún sin reseñas».
		top: color.RGBA{70, 134, 216, 255}, bottom: color.RGBA{16, 40, 90, 255}, motif: seedphotos.MotifPhone,
		catalog: []catSpec{
			{"Revisión", "Diagnóstico", "Software", 0, false}, {"Cambio de pantalla", "Según el modelo", "Pantallas", 90000, true},
			{"Cambio de batería", "Según el modelo", "Baterías", 60000, true}, {"Puerto de carga", "", "Puertos", 40000, true},
		},
	},
}

// addExtras crea las publicaciones con fotos, la foto de perfil, las reseñas y las ofertas extra de un negocio.
// Sin almacenamiento (`store` nil) se saltan las fotos. Devuelve cuántas fotos subió.
func addExtras(tx *gorm.DB, e *models.Entity, x extra, now time.Time, store seedphotos.Uploader, reviewers []models.User, photoDir string) (int, error) {
	uploaded := 0
	if store != nil {
		real := realPhotos(photoDir, e.Name)
		variant := 0
		var profile *uuid.UUID
		for _, p := range x.posts {
			post := models.Post{EntityID: e.ID, Caption: p.caption, CreatedAt: now.Add(-time.Duration(p.agoHours) * time.Hour), UpdatedAt: now}
			if err := tx.Omit("Photos").Create(&post).Error; err != nil {
				return uploaded, fmt.Errorf("publicación: %w", err)
			}
			for j := 0; j < p.photos; j++ {
				data, ct := seedphotos.Sample(x.top, x.bottom, x.motif, variant), "image/jpeg"
				if len(real) > 0 {
					if b, err := os.ReadFile(real[variant%len(real)]); err == nil {
						data, ct = b, contentTypeOf(real[variant%len(real)])
					}
				}
				variant++
				id, err := seedphotos.AddToPost(tx, store, "seed/qr/", &post, j, data, ct)
				if err != nil {
					return uploaded, err
				}
				uploaded++
				if profile == nil {
					profile = &id
				}
			}
		}
		if profile != nil {
			if err := tx.Model(&models.Entity{}).Where("id = ?", e.ID).Update("profile_media_id", *profile).Error; err != nil {
				return uploaded, fmt.Errorf("foto de perfil: %w", err)
			}
		}
	}

	for i, c := range x.catalog {
		svc := models.ServiceItem{EntityID: e.ID, Name: c.name, Description: c.desc, Section: c.section, PriceFrom: c.from, Status: models.ServiceActive, Position: 10 + i}
		if c.price >= 0 {
			price, stamp := c.price, now
			svc.Price, svc.PriceUpdatedAt, svc.ConfirmedAt = &price, &stamp, &stamp
		} else {
			svc.PriceFrom = false
		}
		if err := tx.Create(&svc).Error; err != nil {
			return uploaded, fmt.Errorf("catálogo: %w", err)
		}
	}

	for _, r := range x.reviews {
		at := now.Add(-time.Duration(r.agoDays) * 24 * time.Hour)
		rev := models.Review{EntityID: e.ID, UserID: reviewers[r.who].ID, Rating: r.rating, Comment: r.comment, CreatedAt: at, UpdatedAt: at}
		if err := tx.Omit("User").Create(&rev).Error; err != nil {
			return uploaded, fmt.Errorf("reseña: %w", err)
		}
	}

	if s := x.second; s != nil {
		o := models.Offer{EntityID: e.ID, Type: s.offerType, Title: s.title, Price: s.price, Conditions: s.conditions,
			StartsAt: now, EndsAt: now.Add(s.lasts), TotalSlots: s.slots, RadiusMeters: 3000, RepeatPolicy: models.RepeatOnce}
		if err := tx.Omit("Entity", "Items").Create(&o).Error; err != nil {
			return uploaded, fmt.Errorf("segunda oferta: %w", err)
		}
	}
	for _, p := range x.past {
		ends := now.Add(-time.Duration(p.daysAgo) * 24 * time.Hour)
		o := models.Offer{EntityID: e.ID, Type: p.offerType, Title: p.title, StartsAt: ends.Add(-p.duration), EndsAt: ends,
			TotalSlots: p.slots, UsedSlots: p.used, RadiusMeters: 3000, RepeatPolicy: models.RepeatOnce,
			CreatedAt: ends.Add(-p.duration), UpdatedAt: ends}
		if err := tx.Omit("Entity", "Items").Create(&o).Error; err != nil {
			return uploaded, fmt.Errorf("oferta terminada: %w", err)
		}
	}
	return uploaded, nil
}

// realPhotos: si existe <dir>/<nombre-del-negocio>/ con fotos, se usan esas en vez de las dibujadas.
func realPhotos(dir, name string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	for _, pat := range []string{"*.jpg", "*.jpeg", "*.png", "*.webp"} {
		m, _ := filepath.Glob(filepath.Join(dir, slug(name), pat))
		out = append(out, m...)
	}
	sort.Strings(out)
	return out
}

// slug: «La Sazón de Rosa» → «la-sazon-de-rosa».
func slug(s string) string {
	s = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n", "Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u").Replace(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func contentTypeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
