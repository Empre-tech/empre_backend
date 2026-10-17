// Command seedqr crea 5 negocios de categorías distintas, cada uno con una oferta vigente de un tipo distinto, y
// una cuenta de cliente, para probar de punta a punta el reclamo, el QR y el canje. Además les da contenido de
// ejemplo para que la interfaz se vea viva: publicaciones con fotos (dibujadas o tuyas), foto de perfil, reseñas,
// una segunda oferta vigente y ofertas ya terminadas (para «Cómo te va» del dueño). Un negocio queda sin fotos ni
// reseñas a propósito, para ver también esos casos.
//
// Ejecútalo a mano desde la carpeta empre_backend (usa el mismo .env que la API, que ya debe haber migrado la base):
//
//	go run ./cmd/seedqr
//	go run ./cmd/seedqr -lat 4.6097 -lng -74.0817   // los negocios alrededor de donde estés
//	go run ./cmd/seedqr -photos ./fotos_ejemplo     // tus fotos: ./fotos_ejemplo/la-sazon-de-rosa/*.jpg, etc.
//	go run ./cmd/seedqr -nophotos                    // sin subir nada al almacenamiento
//	go run ./cmd/seedqr -samples ./muestras          // solo guarda las fotos dibujadas en una carpeta y sale
//
// Las fotos se suben al mismo S3 que usa la API (S3_BUCKET del .env); sin S3_BUCKET se saltan.
//
// El reclamo comprueba la distancia si el teléfono manda su ubicación (radio de cada oferta: 3 km), así que si no
// estás en Santa Marta pasa tu ubicación con -lat y -lng. Es re-ejecutable: reutiliza las cuentas y borra en duro
// los negocios anteriores de los dueños (con sus ofertas, reclamos, catálogo y horarios) antes de crearlos de nuevo,
// así las ofertas vuelven a empezar con todos sus cupos y su tiempo.
//
// Cuentas (contraseña demo1234):
//   - Cliente que reclama:  cliente.qr@empre.app
//   - Dueños que escanean:  qr01@empre.app … qr05@empre.app (cada uno, su negocio)
//   - Reseñadores de ejemplo: resena1@empre.app … resena4@empre.app
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const password = "demo1234"

// item es un servicio del catálogo con su precio habitual (en pesos).
type item struct {
	name  string
	price int
}

// shop describe un negocio de prueba y su oferta.
type shop struct {
	owner, category, name, tagline, desc string
	// Desplazamiento en metros desde el centro (norte, este).
	north, east float64
	// Horario: vacío = abierto 24 h todos los días; si no, "HH:MM-HH:MM" todos los días.
	hours string

	offerType  models.OfferType
	title      string
	conditions string
	lasts      time.Duration
	slots      int
	repeat     models.RepeatPolicy
	// Con catálogo: precio especial frente a la suma congelada de los servicios (se ve el habitual tachado).
	catalog []item
	special int
	// Sin catálogo: precio de texto y valor normal opcional (el formulario de siempre).
	price, normal string
}

var shops = []shop{
	{
		owner: "Rosa Medina", category: "Restaurantes", name: "La Sazón de Rosa",
		tagline: "Almuerzo casero con sopa y jugo", desc: "Comida típica costeña hecha en casa.",
		north: 120, east: -80,
		offerType: models.OfferHours, title: "Almuerzo ejecutivo a mitad de precio",
		conditions: "Solo para comer en el local. Incluye jugo del día.", lasts: 3 * time.Hour, slots: 8, repeat: models.RepeatOnce,
		catalog: []item{{"Almuerzo ejecutivo", 22000}}, special: 11000,
	},
	{
		owner: "Julián Barros", category: "Belleza y peluquería", name: "Barbería El Parque",
		tagline: "Cortes clásicos y degradados", desc: "Barbería de barrio con cita o por orden de llegada.",
		north: -150, east: 140,
		offerType: models.OfferDay, title: "Corte + barba",
		conditions: "Válido de lunes a sábado. Llega con 10 minutos de anticipación.", lasts: 8 * time.Hour, slots: 6, repeat: models.RepeatOnce,
		catalog: []item{{"Corte de cabello", 25000}, {"Arreglo de barba", 12000}}, special: 28000,
	},
	{
		owner: "Sofía Pinedo", category: "Cafeterías y postres", name: "Café Sierra Nevada",
		tagline: "Café de la Sierra y pan recién horneado", desc: "Café de origen samario y repostería.",
		north: 260, east: 210, hours: "06:00-21:00",
		// Pocos cupos: para probar «oferta agotada» con varias cuentas.
		offerType: models.OfferHours, title: "Tinto + pandebono", conditions: "Para llevar o en mesa.",
		lasts: 2 * time.Hour, slots: 2, repeat: models.RepeatOnce, price: "$6.000", normal: "$9.000",
	},
	{
		owner: "Martín Ospino", category: "Mascotas", name: "Veterinaria Patitas",
		tagline: "Consulta, vacunas y peluquería canina", desc: "Atención para perros y gatos.",
		north: -320, east: -260,
		// Oferta de la semana que se puede canjear una vez por día: para probar volver a reclamar.
		offerType: models.OfferWeek, title: "Desparasitación con 30 % menos",
		conditions: "Perros y gatos hasta 20 kg. Una vez por día.", lasts: 5 * 24 * time.Hour, slots: 20, repeat: models.RepeatDaily,
		catalog: []item{{"Desparasitación", 50000}}, special: 35000,
	},
	{
		owner: "Kevin Arrieta", category: "Tecnología", name: "CeluFix Express",
		tagline: "Arreglamos tu celular el mismo día", desc: "Pantallas, baterías y puertos de carga.",
		north: 40, east: 420,
		// Termina en 25 minutos: la franja en «última llamada» y un QR que vence con la oferta.
		offerType: models.OfferHours, title: "Revisión gratis + 15 % en batería",
		conditions: "Revisión sin costo; el descuento aplica al cambio de batería.", lasts: 25 * time.Minute, slots: 5, repeat: models.RepeatOnce,
		price: "Gratis",
	},
}

func main() {
	lat := flag.Float64("lat", 11.2350, "latitud del centro (por defecto, Santa Marta)")
	lng := flag.Float64("lng", -74.2050, "longitud del centro")
	photoDir := flag.String("photos", "", "carpeta con tus fotos: <carpeta>/<nombre-del-negocio>/*.jpg (si no, se dibujan)")
	noPhotos := flag.Bool("nophotos", false, "no subir fotos")
	samples := flag.String("samples", "", "guardar las fotos dibujadas en esta carpeta y salir (para verlas)")
	flag.Parse()

	if *samples != "" {
		writeSamples(*samples)
		return
	}

	cfg := config.LoadConfig()
	database.ConnectDB(cfg)
	db := database.DB

	database.SeedCategories(db)
	database.SeedSubcategories(db)

	var categories []models.Category
	if err := db.Find(&categories).Error; err != nil {
		log.Fatal("No se pudieron cargar las categorías: ", err)
	}
	catByName := make(map[string]models.Category, len(categories))
	for _, c := range categories {
		catByName[c.Name] = c
	}

	client := ensureUser(db, "cliente.qr@empre.app", "Cliente de prueba")
	reviewers := make([]models.User, len(reviewerNames))
	for i, n := range reviewerNames {
		reviewers[i] = ensureUser(db, fmt.Sprintf("resena%d@empre.app", i+1), n)
	}
	var store uploader
	if !*noPhotos && cfg.S3Bucket != "" {
		store = services.NewStorageService(cfg)
	} else if !*noPhotos {
		fmt.Println("Sin S3_BUCKET en el .env: se crean los negocios sin fotos.")
	}
	zones := services.NewZoneService(db)
	now := utils.NowInCartagena()
	totalPhotos := 0

	for i, s := range shops {
		cat, ok := catByName[s.category]
		if !ok {
			log.Fatalf("Categoría no encontrada: %s", s.category)
		}
		owner := ensureUser(db, fmt.Sprintf("qr%02d@empre.app", i+1), s.owner)

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := clearOwner(tx, owner.ID); err != nil {
				return fmt.Errorf("limpiar: %w", err)
			}
			plat, plng := offset(*lat, *lng, s.north, s.east)
			entity := models.Entity{
				ID:                 uuid.New(),
				OwnerID:            owner.ID,
				Name:               s.name,
				Description:        s.desc,
				Tagline:            s.tagline,
				CategoryID:         cat.ID,
				City:               "Santa Marta",
				Address:            fmt.Sprintf("Calle %d #%d-%02d", 20+i, 5+i, 10+i*7),
				ContactInfo:        fmt.Sprintf("+57 300 555 01%02d", i+1),
				Latitude:           plat,
				Longitude:          plng,
				VerificationStatus: models.StatusVerified,
				IsVerified:         true,
				ServiceMode:        models.ServiceModeInPlace,
			}
			if err := zones.Assign(&entity); err != nil {
				return fmt.Errorf("zona: %w", err)
			}
			if err := tx.Create(&entity).Error; err != nil {
				return fmt.Errorf("negocio: %w", err)
			}
			if err := tx.Create(hoursFor(entity.ID, s.hours)).Error; err != nil {
				return fmt.Errorf("horario: %w", err)
			}

			offer := models.Offer{
				ID:           uuid.New(),
				EntityID:     entity.ID,
				Type:         s.offerType,
				Title:        s.title,
				Price:        s.price,
				NormalPrice:  s.normal,
				Conditions:   s.conditions,
				StartsAt:     now,
				EndsAt:       now.Add(s.lasts),
				TotalSlots:   s.slots,
				RadiusMeters: 3000,
				RepeatPolicy: s.repeat,
			}
			var frozen []models.OfferItem
			if len(s.catalog) > 0 {
				ref := 0
				for pos, it := range s.catalog {
					price := it.price
					stamp := now
					svc := models.ServiceItem{EntityID: entity.ID, Name: it.name, Price: &price, PriceUpdatedAt: &stamp, ConfirmedAt: &stamp, Status: models.ServiceActive, Position: pos}
					if err := tx.Create(&svc).Error; err != nil {
						return fmt.Errorf("catálogo: %w", err)
					}
					id := svc.ID
					frozen = append(frozen, models.OfferItem{ServiceItemID: &id, Name: it.name, Price: &price, Position: pos})
					ref += it.price
				}
				special := s.special
				offer.SpecialPrice = &special
				offer.ReferencePrice = &ref
			}
			if err := tx.Omit("Entity", "Items").Create(&offer).Error; err != nil {
				return fmt.Errorf("oferta: %w", err)
			}
			for j := range frozen {
				frozen[j].OfferID = offer.ID
			}
			if len(frozen) > 0 {
				if err := tx.Create(&frozen).Error; err != nil {
					return fmt.Errorf("servicios de la oferta: %w", err)
				}
			}
			n, err := addExtras(tx, &entity, extras[i], now, store, reviewers, *photoDir)
			if err != nil {
				return err
			}
			totalPhotos += n
			x := extras[i]
			fmt.Printf("  %-22s %-22s %-7s %2d cupos · termina %s · %d fotos · %d reseñas · dueño qr%02d@empre.app\n",
				s.name, s.category, s.offerType, s.slots, offer.EndsAt.Format("02/01 15:04"), n, len(x.reviews), i+1)
			return nil
		})
		if err != nil {
			log.Fatalf("%s: %v", s.name, err)
		}
	}

	fmt.Printf("\nListo: %d negocios con oferta alrededor de %.5f, %.5f (radio de reclamo: 3 km).\n", len(shops), *lat, *lng)
	fmt.Printf("Reclama con %s y escanea con el dueño de cada negocio. Contraseña de todas: %s\n", client.Email, password)
	fmt.Printf("Contenido de ejemplo: %d fotos subidas, reseñas de resena1..resena%d@empre.app y ofertas terminadas.\n", totalPhotos, len(reviewerNames))
}

// writeSamples guarda las fotos dibujadas de cada negocio en una carpeta, para verlas sin tocar la base.
func writeSamples(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	for i, s := range shops {
		x := extras[i]
		for v := 0; v < 3; v++ {
			name := filepath.Join(dir, fmt.Sprintf("%s-%d.jpg", slug(s.name), v+1))
			if err := os.WriteFile(name, samplePhoto(x.top, x.bottom, x.motif, v), 0o644); err != nil {
				log.Fatal(err)
			}
		}
	}
	fmt.Println("Fotos de ejemplo guardadas en", dir)
}

// ensureUser devuelve la cuenta con ese correo o la crea.
func ensureUser(db *gorm.DB, email, name string) models.User {
	var user models.User
	// Find en vez de First: que no exista todavía no es un error que deba ensuciar la salida.
	if err := db.Where("email = ?", email).Limit(1).Find(&user).Error; err != nil {
		log.Fatalf("No se pudo buscar %s: %v", email, err)
	}
	if user.ID != uuid.Nil {
		return user
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	user = models.User{ID: uuid.New(), Name: name, Email: email, PasswordHash: string(hash), Role: models.RoleUser}
	if err := db.Create(&user).Error; err != nil {
		log.Fatalf("No se pudo crear %s: %v", email, err)
	}
	return user
}

// clearOwner borra en duro los negocios del dueño y todo lo que cuelga de ellos, en el orden que piden las llaves.
func clearOwner(tx *gorm.DB, owner uuid.UUID) error {
	const mine = "(SELECT id FROM entities WHERE owner_id = ?)"
	var mediaIDs []uuid.UUID
	if err := tx.Raw("SELECT media_id FROM entity_photos WHERE entity_id IN "+mine, owner).Scan(&mediaIDs).Error; err != nil {
		return err
	}
	stmts := []string{
		"UPDATE entities SET profile_media_id = NULL, banner_media_id = NULL WHERE owner_id = ?",
		"DELETE FROM entity_photos WHERE entity_id IN " + mine,
		"DELETE FROM posts WHERE entity_id IN " + mine,
		"DELETE FROM reviews WHERE entity_id IN " + mine,
		"DELETE FROM offer_claims WHERE entity_id IN " + mine,
		"DELETE FROM offer_items WHERE offer_id IN (SELECT id FROM offers WHERE entity_id IN " + mine + ")",
		"DELETE FROM offers WHERE entity_id IN " + mine,
		"DELETE FROM service_items WHERE entity_id IN " + mine,
		"DELETE FROM business_hours WHERE entity_id IN " + mine,
		"DELETE FROM favorites WHERE entity_id IN " + mine,
		"DELETE FROM entities WHERE owner_id = ?",
	}
	for _, q := range stmts {
		if err := tx.Exec(q, owner).Error; err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	if len(mediaIDs) > 0 {
		if err := tx.Exec("DELETE FROM media WHERE id IN ?", mediaIDs).Error; err != nil {
			return fmt.Errorf("medios: %w", err)
		}
	}
	return nil
}

// hoursFor arma los 7 días: abierto 24 h, o el mismo horario todos los días ("06:00-21:00").
func hoursFor(entityID uuid.UUID, span string) []models.BusinessHour {
	out := make([]models.BusinessHour, 0, 7)
	for d := 0; d < 7; d++ {
		h := models.BusinessHour{EntityID: entityID, Weekday: d, Is24h: span == ""}
		if span != "" {
			h.OpenTime, h.CloseTime = span[:5], span[6:]
		}
		out = append(out, h)
	}
	return out
}

// offset mueve un punto unos metros al norte y al este.
func offset(lat, lng, north, east float64) (float64, float64) {
	return lat + north/111_320, lng + east/(111_320*math.Cos(lat*math.Pi/180))
}
