// Command seeddemo creates 20 demo businesses in Santa Marta, each owned by a
// DIFFERENT demo user (with a realistic owner name), spread across the city
// with two tight clusters (to see how overlapping pins behave) and several
// with active offers/promotions.
//
// Run it manually from the empre_backend folder:
//
//	go run ./cmd/seeddemo
//
// It is idempotent: each demo user (demo01..demo20@empre.app, password
// "demo1234") is reused and its previous businesses are hard-deleted before
// recreating them. It uses the same .env as the API.
package main

import (
	"fmt"
	"log"
	"time"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// biz describe un negocio demo. offerType vacío = sin relámpago.
type biz struct {
	owner      string
	category   string
	name       string
	desc       string
	lat, lng   float64
	offerType  models.OfferType
	offerTitle string
	offerPrice string
	promo      bool
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
		// Cluster A — Centro Histórico (4 muy juntos, ~30 m entre sí).
		{"Carlos Rodríguez", "Restaurantes", "Casa Canoas", "Comida típica samaria frente al parque.", 11.24200, -74.21100, models.OfferHours, "Almuerzo típico 2x1", "$14.000", false},
		{"Andrés Felipe Torres", "Belleza y peluquería", "Barbería Samaria", "Cortes clásicos y degradados.", 11.24202, -74.21103, models.OfferDay, "Corte + barba", "$30.000", false},
		{"Camila Sánchez", "Cafeterías y postres", "Café Quinta de San Pedro", "Café de origen y postres artesanales.", 11.24201, -74.21098, "", "", "", false},
		{"Diego Ramírez", "Tiendas y comercio", "Almacén El Faro", "Ropa, calzado y accesorios.", 11.24203, -74.21101, models.OfferHours, "20% en camisas de lino", "$45.000", false},
		// Cluster B — El Rodadero (3 juntos).
		{"Santiago Herrera", "Bares y vida nocturna", "Beach Club Tayrona", "Cócteles y música frente al mar.", 11.20300, -74.22900, models.OfferHours, "2x1 en mojitos", "$22.000", false},
		{"Valentina Díaz", "Salud y bienestar", "Droguería del Mar", "Farmacia y cuidado personal.", 11.20297, -74.22903, "", "", "", false},
		{"Felipe Vargas", "Automotriz", "Taller Rodadero Motors", "Mecánica y alineación.", 11.20299, -74.22897, models.OfferDay, "Alineación + balanceo", "$80.000", false},
		// Dispersos.
		{"Daniela Rojas", "Deporte y recreación", "Gimnasio Fit Beach", "Entrenamiento funcional.", 11.20500, -74.22500, models.OfferHours, "Día de prueba gratis", "$0", false},
		{"Nicolás Moreno", "Turismo y hospedaje", "Hotel Playa Blanca", "Hospedaje con vista al mar.", 11.19000, -74.23000, "", "", "", true},
		{"Gabriela Mendoza", "Educación", "Colegio de Idiomas Samario", "Clases de inglés y francés.", 11.23700, -74.20600, "", "", "", false},
		{"Sebastián Ortega", "Tecnología", "ReparaTech Santa Marta", "Reparación de celulares.", 11.21000, -74.21800, models.OfferDay, "Cambio de pantalla", "$90.000", false},
		{"Alejandra Navarro", "Mascotas", "Veterinaria Colibrí", "Atención y peluquería canina.", 11.22300, -74.20200, models.OfferHours, "Consulta + desparasitación", "$35.000", false},
		{"Mateo Gil", "Servicios del hogar", "Plomería del Caribe", "Plomería a domicilio.", 11.23000, -74.21300, "", "", "", true},
		{"Laura Restrepo", "Fotografía y diseño", "FotoStudio Tayrona", "Fotografía de producto y retrato.", 11.21500, -74.21000, "", "", "", false},
		{"Esteban Cárdenas", "Eventos y fiestas", "Eventos Magdalena", "Decoración y organización.", 11.22600, -74.20800, "", "", "", false},
		{"Paula Salazar", "Otros", "Miscelánea El Camellón", "Artículos de aseo y uso diario.", 11.24100, -74.21000, models.OfferHours, "10% en aseo", "$8.000", false},
		{"María Fernanda Pérez", "Restaurantes", "Pescadería El Morro", "Mariscos frescos en Taganga.", 11.26600, -74.19100, models.OfferDay, "Plato de pescado + jugo", "$18.000", false},
		{"Juan Pablo Gómez", "Cafeterías y postres", "Café Bello Horizonte", "Café y repostería.", 11.18600, -74.22900, models.OfferHours, "Café + postre", "$9.000", false},
		{"Luisa Martínez", "Belleza y peluquería", "Spa Mamatoco", "Masajes y tratamientos.", 11.24700, -74.19600, "", "", "", true},
		{"Isabella Castro", "Tiendas y comercio", "Ferretería Gaira", "Herramientas y materiales.", 11.19200, -74.21100, "", "", "", false},
	}

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
		if err := db.Create(&entity).Error; err != nil {
			log.Fatal("No se pudo crear el negocio: ", err)
		}

		if b.offerType != "" {
			endsAt := now.Add(3 * time.Hour)
			if b.offerType == models.OfferDay {
				endsAt = now.Add(8 * time.Hour)
			}
			offer := models.Offer{
				ID:           uuid.New(),
				EntityID:     entity.ID,
				Type:         b.offerType,
				Title:        b.offerTitle,
				Price:        b.offerPrice,
				StartsAt:     now,
				EndsAt:       endsAt,
				TotalSlots:   10,
				UsedSlots:    0,
				RadiusMeters: 2000,
				RepeatPolicy: models.RepeatOnce,
			}
			if err := db.Create(&offer).Error; err != nil {
				log.Fatal("No se pudo crear la oferta: ", err)
			}
		}

		if b.promo {
			promo := models.Promotion{
				ID:          uuid.New(),
				EntityID:    entity.ID,
				Title:       fmt.Sprintf("Promoción en %s", b.name),
				Description: b.desc,
				Discount:    "15%",
				StartsAt:    now,
				EndsAt:      now.Add(14 * 24 * time.Hour),
				Status:      models.PromotionActive,
			}
			if err := db.Create(&promo).Error; err != nil {
				log.Fatal("No se pudo crear la promoción: ", err)
			}
		}
	}

	fmt.Printf("Listo: %d negocios demo creados en Santa Marta, cada uno con dueño distinto (demo01..demo%02d@empre.app / demo1234).\n", len(list), len(list))
	fmt.Println("Hay dos clusters (Centro Histórico y El Rodadero) y el resto dispersos por Santa Marta.")
}

// clearDemo borra en duro los negocios (y sus ofertas/promociones) del usuario
// demo, para que el comando sea re-ejecutable sin duplicar.
func clearDemo(db *gorm.DB, uid uuid.UUID) {
	db.Exec(`DELETE FROM offers WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM promotions WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM business_hours WHERE entity_id IN (SELECT id FROM entities WHERE owner_id = ?)`, uid)
	db.Exec(`DELETE FROM entities WHERE owner_id = ?`, uid)
}
