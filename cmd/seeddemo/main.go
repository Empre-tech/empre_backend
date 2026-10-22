// Command seeddemo creates 20 demo businesses in Santa Marta, each owned by a
// DIFFERENT demo user (with a realistic owner name), covering the 16
// categories and spread across the city's zones (Centro, El Rodadero, Taganga
// and the sectors without a zone), with two tight clusters (to see how
// overlapping pins behave). Every business has an active offer with a special
// price and its usual price, ending at different times (one in under an hour,
// to see the urgent badge) so the map and the Offers tab can be reviewed.
// Half of them (the even-indexed ones) also get a profile photo and a post
// with 3 sample photos drawn in their category's color, so cards and pins can
// be judged with images; the other half keep the icon-only look.
//
// Run it manually from the empre_backend folder:
//
//	go run ./cmd/seeddemo
//
// It is idempotent: each demo user (demo01..demo20@empre.app, password
// "demo1234") is reused and its previous businesses are hard-deleted before
// recreating them (with their posts, photos and media). It uses the same
// .env as the API; photos go to its S3 (S3_BUCKET) and are skipped without it.
package main

import (
	"fmt"
	"log"
	"time"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/models"
	"empre_backend/internal/seedphotos"
	"empre_backend/internal/services"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// biz describe un negocio demo con su oferta: precio especial, precio habitual y cuánto falta para que termine.
type biz struct {
	owner      string
	category   string
	name       string
	desc       string
	lat, lng   float64
	offerType  models.OfferType
	offerTitle string
	special    int
	usual      int
	endsIn     time.Duration
}

func main() {
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

	now := time.Now()

	list := []biz{
		// Centro Histórico: 4 muy juntos (~30 m entre sí) para ver la pila de pines.
		{"Carlos Rodríguez", "Restaurantes", "Casa Canoas", "Comida típica samaria frente al parque.", 11.24200, -74.21100, models.OfferHours, "Almuerzo típico 2x1", 14000, 28000, 2 * time.Hour},
		{"Andrés Felipe Torres", "Belleza y peluquería", "Barbería Samaria", "Cortes clásicos y degradados.", 11.24202, -74.21103, models.OfferDay, "Corte + barba", 30000, 38000, 9 * time.Hour},
		{"Camila Sánchez", "Cafeterías y postres", "Café Quinta de San Pedro", "Café de origen y postres artesanales.", 11.24201, -74.21098, models.OfferHours, "2x1 en capuchino", 8000, 16000, 40 * time.Minute},
		{"Diego Ramírez", "Tiendas y comercio", "Almacén El Faro", "Ropa, calzado y accesorios.", 11.24203, -74.21101, models.OfferWeek, "Camisas de lino 25 % menos", 45000, 60000, 5 * 24 * time.Hour},
		// Centro, dispersos.
		{"Gabriela Mendoza", "Educación", "Colegio de Idiomas Samario", "Clases de inglés y francés.", 11.23700, -74.20600, models.OfferWeek, "Primera clase de inglés a mitad", 25000, 50000, 6 * 24 * time.Hour},
		{"Paula Salazar", "Otros", "Miscelánea El Camellón", "Artículos de aseo y uso diario.", 11.24100, -74.21000, models.OfferDay, "Kit de aseo con 20 % menos", 32000, 40000, 7 * time.Hour},
		{"Esteban Cárdenas", "Eventos y fiestas", "Eventos Magdalena", "Decoración y organización.", 11.23900, -74.20900, models.OfferWeek, "Decoración de cumpleaños", 180000, 240000, 4 * 24 * time.Hour},
		// El Rodadero: 3 juntos.
		{"Santiago Herrera", "Bares y vida nocturna", "Beach Club Tayrona", "Cócteles y música frente al mar.", 11.20300, -74.22900, models.OfferHours, "2x1 en mojitos", 22000, 44000, 3 * time.Hour},
		{"Valentina Díaz", "Salud y bienestar", "Droguería del Mar", "Farmacia y cuidado personal.", 11.20297, -74.22903, models.OfferDay, "Bloqueador solar 30 % menos", 42000, 60000, 10 * time.Hour},
		{"Felipe Vargas", "Automotriz", "Taller Rodadero Motors", "Mecánica y alineación.", 11.20299, -74.22897, models.OfferDay, "Alineación + balanceo", 80000, 110000, 8 * time.Hour},
		// El Rodadero, dispersos.
		{"Daniela Rojas", "Deporte y recreación", "Gimnasio Fit Beach", "Entrenamiento funcional.", 11.20500, -74.22500, models.OfferWeek, "Semana de prueba", 30000, 60000, 6 * 24 * time.Hour},
		{"Nicolás Moreno", "Turismo y hospedaje", "Hotel Playa Blanca", "Hospedaje con vista al mar.", 11.20800, -74.22700, models.OfferWeek, "Noche entre semana con desayuno", 210000, 280000, 5 * 24 * time.Hour},
		// Taganga.
		{"María Fernanda Pérez", "Restaurantes", "Pescadería El Morro", "Mariscos frescos en Taganga.", 11.26600, -74.19100, models.OfferDay, "Plato de pescado + jugo", 18000, 26000, 6 * time.Hour},
		{"Tomás Ríos", "Deporte y recreación", "Taganga Dive", "Buceo y snorkel en el Tayrona.", 11.26750, -74.19250, models.OfferWeek, "Bautizo de buceo", 180000, 240000, 3 * 24 * time.Hour},
		{"Lucía Ospina", "Bares y vida nocturna", "Bar La Bahía", "Atardeceres y música en vivo.", 11.26500, -74.19000, models.OfferHours, "Cerveza artesanal 2x1", 12000, 24000, 50 * time.Minute},
		// Sectores sin zona: Mamatoco, Bello Horizonte, Gaira y alrededores.
		{"Sebastián Ortega", "Tecnología", "ReparaTech Santa Marta", "Reparación de celulares.", 11.21000, -74.21800, models.OfferDay, "Cambio de pantalla", 90000, 120000, 11 * time.Hour},
		{"Alejandra Navarro", "Mascotas", "Veterinaria Colibrí", "Atención y peluquería canina.", 11.22300, -74.20200, models.OfferHours, "Consulta + desparasitación", 35000, 50000, 4 * time.Hour},
		{"Mateo Gil", "Servicios del hogar", "Plomería del Caribe", "Plomería a domicilio.", 11.23000, -74.21300, models.OfferWeek, "Revisión de fugas a domicilio", 40000, 60000, 4 * 24 * time.Hour},
		{"Laura Restrepo", "Fotografía y diseño", "FotoStudio Tayrona", "Fotografía de producto y retrato.", 11.21500, -74.21000, models.OfferWeek, "Sesión de retrato 30 min", 70000, 100000, 5 * 24 * time.Hour},
		{"Luisa Martínez", "Salud y bienestar", "Spa Mamatoco", "Masajes y tratamientos.", 11.24700, -74.19600, models.OfferDay, "Masaje relajante 45 min", 60000, 85000, 9 * time.Hour},
	}

	var store seedphotos.Uploader
	if cfg.S3Bucket != "" {
		store = services.NewStorageService(cfg)
	} else {
		fmt.Println("Sin S3_BUCKET en el .env: se crean los negocios demo sin fotos.")
	}

	zones := services.NewZoneService(db)
	withPhotos := 0
	for i, b := range list {
		email := fmt.Sprintf("demo%02d@empre.app", i+1)

		var user models.User
		if err := db.Where("email = ?", email).First(&user).Error; err != nil {
			hash, herr := bcrypt.GenerateFromPassword([]byte("demo1234"), bcrypt.MinCost)
			if herr != nil {
				log.Fatal(herr)
			}
			user = models.User{
				ID:           uuid.New(),
				Name:         b.owner,
				Email:        email,
				PasswordHash: string(hash),
				Role:         models.RoleUser,
			}
			if err := db.Create(&user).Error; err != nil {
				log.Fatal("No se pudo crear el usuario demo: ", err)
			}
		}

		clearDemo(db, user.ID)

		cat, ok := catByName[b.category]
		if !ok {
			log.Fatalf("Categoría no encontrada: %s", b.category)
		}

		entity := models.Entity{
			ID:                 uuid.New(),
			OwnerID:            user.ID,
			Name:               b.name,
			Description:        b.desc,
			CategoryID:         cat.ID,
			City:               "Santa Marta",
			Address:            fmt.Sprintf("Cra %d #%d", 1+i, 10+i),
			ContactInfo:        "+57 300 000 0000",
			Latitude:           b.lat,
			Longitude:          b.lng,
			VerificationStatus: models.StatusVerified,
			IsVerified:         true,
			ServiceMode:        models.ServiceModeInPlace,
		}
		if err := zones.Assign(&entity); err != nil {
			log.Fatal("No se pudo asignar la zona: ", err)
		}
		if err := db.Create(&entity).Error; err != nil {
			log.Fatal("No se pudo crear el negocio: ", err)
		}

		sp, rp := b.special, b.usual
		offer := models.Offer{
			ID:             uuid.New(),
			EntityID:       entity.ID,
			Type:           b.offerType,
			Title:          b.offerTitle,
			Price:          pesos(b.special),
			NormalPrice:    pesos(b.usual),
			SpecialPrice:   &sp,
			ReferencePrice: &rp,
			StartsAt:       now.Add(-10 * time.Minute),
			EndsAt:         now.Add(b.endsIn),
			TotalSlots:     10 + i%3*5,
			UsedSlots:      0,
			RadiusMeters:   3000,
			RepeatPolicy:   models.RepeatOnce,
		}
		if err := db.Create(&offer).Error; err != nil {
			log.Fatal("No se pudo crear la oferta: ", err)
		}

		if store != nil && i%2 == 0 {
			if err := db.Transaction(func(tx *gorm.DB) error { return addPhotos(tx, store, &entity, cat, now) }); err != nil {
				log.Fatalf("%s: %v", b.name, err)
			}
			withPhotos++
		}
	}

	fmt.Printf("Listo: %d negocios demo en Santa Marta, cada uno con su oferta y dueño distinto (demo01..demo%02d@empre.app / demo1234).\n", len(list), len(list))
	fmt.Println("Pilas de pines en Centro Histórico y El Rodadero; también hay negocios en Taganga y en sectores sin zona.")
	if store != nil {
		fmt.Printf("Fotos: %d negocios con foto de perfil y una publicación de %d fotos; el resto, solo con el ícono.\n", withPhotos, photosPerPost)
	}
}

const photosPerPost = 3

// motifs: el dibujo de las fotos de ejemplo por categoría; las demás llevan solo el fondo con su color.
var motifs = map[string]seedphotos.Motif{
	"Restaurantes":         seedphotos.MotifPlate,
	"Cafeterías y postres": seedphotos.MotifCup,
	"Belleza y peluquería": seedphotos.MotifPole,
	"Mascotas":             seedphotos.MotifPaw,
	"Tecnología":           seedphotos.MotifPhone,
}

// addPhotos crea una publicación con 3 fotos dibujadas en el color de la categoría y usa la primera como foto de perfil.
func addPhotos(tx *gorm.DB, store seedphotos.Uploader, e *models.Entity, cat models.Category, now time.Time) error {
	post := models.Post{EntityID: e.ID, Caption: "Así se ve " + e.Name, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now}
	if err := tx.Omit("Photos").Create(&post).Error; err != nil {
		return fmt.Errorf("publicación: %w", err)
	}
	top, bottom := seedphotos.Gradient(cat.Color)
	var profile uuid.UUID
	for j := 0; j < photosPerPost; j++ {
		id, err := seedphotos.AddToPost(tx, store, "seed/demo/", &post, j, seedphotos.Sample(top, bottom, motifs[cat.Name], j), "image/jpeg")
		if err != nil {
			return err
		}
		if j == 0 {
			profile = id
		}
	}
	if err := tx.Model(&models.Entity{}).Where("id = ?", e.ID).Update("profile_media_id", profile).Error; err != nil {
		return fmt.Errorf("foto de perfil: %w", err)
	}
	return nil
}

// pesos: 25000 → «$25.000».
func pesos(n int) string {
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += "."
		}
		out += string(r)
	}
	return "$" + out
}

// clearDemo borra en duro los negocios (y sus ofertas/promociones,
// publicaciones, fotos y medios) del usuario demo, para que el comando sea
// re-ejecutable sin duplicar.
func clearDemo(db *gorm.DB, uid uuid.UUID) {
	const mine = "(SELECT id FROM entities WHERE owner_id = ?)"
	var mediaIDs []uuid.UUID
	db.Raw("SELECT media_id FROM entity_photos WHERE entity_id IN "+mine+" UNION SELECT profile_media_id FROM entities WHERE owner_id = ? AND profile_media_id IS NOT NULL", uid, uid).Scan(&mediaIDs)
	db.Exec(`UPDATE entities SET profile_media_id = NULL, banner_media_id = NULL WHERE owner_id = ?`, uid)
	db.Exec(`DELETE FROM entity_photos WHERE entity_id IN `+mine, uid)
	db.Exec(`DELETE FROM posts WHERE entity_id IN `+mine, uid)
	db.Exec(`DELETE FROM offer_claims WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM offer_items WHERE offer_id IN (SELECT o.id FROM offers o JOIN entities e ON e.id = o.entity_id WHERE e.owner_id = ?)`, uid)
	db.Exec(`DELETE FROM offers WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM promotions WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM business_hours WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM entities WHERE owner_id = ?`, uid)
	if len(mediaIDs) > 0 {
		db.Exec(`DELETE FROM media WHERE id IN ?`, mediaIDs)
	}
}
