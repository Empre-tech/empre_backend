// Command seedbarrio prepara los negocios demo de Santa Marta para revisar la pestaña Ofertas del barrio (3.48)
// sin borrar nada: vuelve a poner vigentes las ofertas demo que ya terminaron, les escribe la nota de quien atiende,
// pone el nombre de quien atiende y un horario por tipo (si no tiene) en cada negocio, y crea unos pocos canjes de este mes con su reseña verificada
// (para «Lo recomiendan tus vecinos» y «Este mes ahorraste» de demo01). Antes hay que haber corrido cmd/seeddemo.
//
// Desde la carpeta empre_backend:
//
//	go run ./cmd/seedbarrio
//
// Se puede correr varias veces: solo alarga las ofertas vencidas y no repite canjes ni reseñas.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"empre_backend/config"
	"empre_backend/internal/database"
	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// La nota de cada negocio demo, con la voz de quien atiende (hasta 90 caracteres).
var notes = map[string]string{
	"Casa Canoas":                "Hoy hay sancocho de gallina. Llega antes de la 1, que se acaba.",
	"Barbería Samaria":           "Corte con navaja y toalla caliente, como en los viejos tiempos.",
	"Café Quinta de San Pedro":   "Café de la Sierra Nevada, tostado aquí mismo esta mañana.",
	"Almacén El Faro":            "Lino fresco para el calor samario. Te ayudo con la talla.",
	"Colegio de Idiomas Samario": "La primera clase es para que conozcas el método. Sin compromiso.",
	"Miscelánea El Camellón":     "Todo lo de la casa en un solo lugar, y te lo empaco rápido.",
	"Eventos Magdalena":          "Te armo la decoración y tú solo disfrutas la fiesta.",
	"Beach Club Tayrona":         "El mejor atardecer del Rodadero, con tu mojito en la mano.",
	"Droguería del Mar":          "Protégete del sol samario. Te recomiendo el que va con tu piel.",
	"Taller Rodadero Motors":     "Te lo entrego el mismo día y con la prueba en la vía.",
	"Gimnasio Fit Beach":         "Entrena frente al mar. La primera semana te armo la rutina yo.",
	"Hotel Playa Blanca":         "Despierta con el mar en la ventana y el desayuno listo.",
	"Pescadería El Morro":        "Pescado de esta mañana, traído por los pescadores de Taganga.",
	"Taganga Dive":               "Tu primera inmersión en el Tayrona, con instructor a tu lado.",
	"Bar La Bahía":               "Música en vivo desde las 6. La cerveza es de aquí de la costa.",
	"ReparaTech Santa Marta":     "Pantalla original y garantía. Mientras esperas, te tomas un tinto.",
	"Veterinaria Colibrí":        "Tu mascota sale feliz y desparasitada. La conocemos por su nombre.",
	"Plomería del Caribe":        "Llego a tu casa en menos de una hora en Santa Marta.",
	"FotoStudio Tayrona":         "Luz natural del Caribe y fotos listas al día siguiente.",
	"Spa Mamatoco":               "Aceites de coco de aquí de la costa. Sales nuevo.",
}

// Horario de cada tipo de negocio demo (3.49: las rutas solo proponen paradas abiertas a su hora). Abre todos los días.
var hoursByCategory = map[string][2]string{
	"Restaurantes":          {"11:00", "22:00"},
	"Cafeterías y postres":  {"07:00", "20:00"},
	"Bares y vida nocturna": {"17:00", "02:00"},
	"Deporte y recreación":  {"05:00", "21:00"},
	"Salud y bienestar":     {"08:00", "20:00"},
	"Belleza y peluquería":  {"09:00", "19:00"},
	"Turismo y hospedaje":   {"00:00", "23:59"},
	"Eventos y fiestas":     {"09:00", "18:00"},
}

// Reseñas verificadas: quién canjeó (usuario demo), en qué negocio y qué dijo.
var voices = []struct {
	reviewer int // demoNN
	business string
	rating   int
	comment  string
	daysAgo  int
}{
	{3, "Barbería Samaria", 5, "Andrés te deja como nuevo, y con la oferta sale regalado.", 3},
	{4, "Café Quinta de San Pedro", 5, "El mejor café del Centro. Camila se acuerda de cómo lo tomas.", 6},
	{5, "Casa Canoas", 5, "Sabe a almuerzo de la casa de mi abuela.", 1},
	{6, "Gimnasio Fit Beach", 4, "Daniela tiene paciencia infinita y la rutina me sirvió de verdad.", 9},
	// Canjes de demo01 este mes: su «Este mes ahorraste».
	{1, "Café Quinta de San Pedro", 5, "Capuchino perfecto y la atención de siempre.", 2},
	{1, "Barbería Samaria", 4, "Buen corte y rápido, volveré.", 4},
	{1, "Colegio de Idiomas Samario", 5, "La clase de prueba me convenció de inscribirme.", 5},
}

func main() {
	cfg := config.LoadConfig()
	database.ConnectDB(cfg)
	db := database.DB
	now := time.Now()

	var ents []models.Entity
	if err := db.Joins("JOIN users u ON u.id = entities.owner_id").Where("u.email LIKE 'demo%@empre.app'").
		Preload("Owner").Preload("Category").Preload("Hours").Find(&ents).Error; err != nil {
		log.Fatal(err)
	}
	if len(ents) == 0 {
		log.Fatal("No hay negocios demo: corre primero go run ./cmd/seeddemo")
	}
	byName := map[string]models.Entity{}
	reopened := 0
	for i, e := range ents {
		byName[e.Name] = e
		if len(e.Hours) == 0 {
			h, ok := hoursByCategory[e.Category.Name]
			if !ok {
				h = [2]string{"08:00", "19:00"}
			}
			for d := 0; d < 7; d++ {
				db.Create(&models.BusinessHour{ID: uuid.New(), EntityID: e.ID, Weekday: d, OpenTime: h[0], CloseTime: h[1]})
			}
		}
		first := strings.Fields(e.Owner.Name)
		if len(first) > 0 && e.AttendantName == "" { // vacío o NULL (columna nueva)
			db.Model(&models.Entity{}).Where("id = ?", e.ID).Update("attendant_name", first[0])
		}
		if n, ok := notes[e.Name]; ok {
			db.Model(&models.Offer{}).Where("entity_id = ? AND coalesce(owner_note, '') = ''", e.ID).Update("owner_note", n)
		}
		if e.Name == "Café Ribera" {
			continue // es el negocio sin oferta de 3.50: su oferta vieja debe seguir terminada
		}
		// Ofertas vencidas → vigentes otra vez, con finales escalonados (una en menos de una hora) y libres.
		ends := []time.Duration{50 * time.Minute, 3 * time.Hour, 7 * time.Hour, 26 * time.Hour, 4 * 24 * time.Hour}[i%5]
		res := db.Model(&models.Offer{}).Where("entity_id = ? AND ends_at <= ?", e.ID, now).
			Updates(map[string]any{"starts_at": now.Add(-30 * time.Minute), "ends_at": now.Add(ends), "used_slots": 0})
		reopened += int(res.RowsAffected)
	}
	// Dos ofertas «nuevas esta semana» para la pasarela.
	for _, name := range []string{"Gimnasio Fit Beach", "Veterinaria Colibrí"} {
		if e, ok := byName[name]; ok {
			db.Model(&models.Offer{}).Where("entity_id = ?", e.ID).Update("created_at", now.Add(-26*time.Hour))
		}
	}

	made := 0
	for _, v := range voices {
		e, ok := byName[v.business]
		if !ok {
			continue
		}
		var user models.User
		if err := db.Where("email = ?", fmt.Sprintf("demo%02d@empre.app", v.reviewer)).First(&user).Error; err != nil {
			continue
		}
		var exists int64
		db.Model(&models.Review{}).Where("entity_id = ? AND user_id = ?", e.ID, user.ID).Count(&exists)
		if exists > 0 {
			continue
		}
		var offer models.Offer
		if err := db.Where("entity_id = ?", e.ID).Order("ends_at DESC").First(&offer).Error; err != nil {
			continue
		}
		if err := redeemAndReview(db, user, e, offer, v.rating, v.comment, now.AddDate(0, 0, -v.daysAgo)); err != nil {
			log.Fatal(err)
		}
		made++
	}
	cafe := seedReviewedCafe(db, now)
	fmt.Printf("Listo: %d ofertas demo vigentes otra vez, notas y quién atiende en %d negocios, %d canjes con reseña verificada.\n", reopened, len(ents), made)
	fmt.Println(cafe)
}

// seedReviewedCafe (3.50): «Café Ribera», un negocio del Centro SIN oferta vigente, con tres reseñas verificadas (de
// una oferta que ya terminó), un servicio de 30 min con precio y la agenda encendida. Así una ruta puede llevarlo como
// parada de servicio («precio de siempre», entra por sus reseñas) y se puede pedir hora. Lo crea una sola vez.
func seedReviewedCafe(db *gorm.DB, now time.Time) string {
	var owner models.User
	if err := db.Where("email = ?", "demo04@empre.app").First(&owner).Error; err != nil {
		return "Café Ribera: falta demo04 (corre cmd/seeddemo)"
	}
	var e models.Entity
	if err := db.Where("owner_id = ? AND name = ?", owner.ID, "Café Ribera").First(&e).Error; err == nil {
		return "Café Ribera ya existe"
	}
	var cat models.Category
	if err := db.Where("name = ?", "Cafeterías y postres").First(&cat).Error; err != nil {
		return "Café Ribera: falta la categoría"
	}
	e = models.Entity{
		ID: uuid.New(), OwnerID: owner.ID, Name: "Café Ribera", Description: "Café de la Sierra Nevada, tostado en el local.",
		CategoryID: cat.ID, City: "Santa Marta", Address: "Cra 2 #18", ContactInfo: "+57 300 000 0000",
		Latitude: 11.24150, Longitude: -74.21060, VerificationStatus: models.StatusVerified, IsVerified: true,
		ServiceMode: models.ServiceModeInPlace, AttendantName: "Ana",
	}
	if err := services.NewZoneService(db).Assign(&e); err != nil {
		log.Fatal(err)
	}
	if err := db.Create(&e).Error; err != nil {
		log.Fatal(err)
	}
	for d := 0; d < 7; d++ {
		db.Create(&models.BusinessHour{ID: uuid.New(), EntityID: e.ID, Weekday: d, OpenTime: "07:00", CloseTime: "20:00"})
	}
	price, dur := 9000, 30
	db.Create(&models.ServiceItem{ID: uuid.New(), EntityID: e.ID, Name: "Dos cafés de origen", Price: &price, DurationMin: &dur, Bookable: true, Status: models.ServiceActive})
	db.Create(&models.AgendaSettings{ID: uuid.New(), EntityID: e.ID, Enabled: true, SlotMinutes: 15, MinNoticeMin: 15, HorizonDays: 14})
	// Una oferta que ya terminó, con tres canjes y sus reseñas verificadas.
	sp, rp := 7000, 9000
	old := models.Offer{ID: uuid.New(), EntityID: e.ID, Type: models.OfferDay, Title: "Café de bienvenida", Price: "$7.000", NormalPrice: "$9.000",
		SpecialPrice: &sp, ReferencePrice: &rp, StartsAt: now.AddDate(0, 0, -12), EndsAt: now.AddDate(0, 0, -10), TotalSlots: 20, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
	if err := db.Create(&old).Error; err != nil {
		log.Fatal(err)
	}
	for i, v := range []struct {
		n       int
		rating  int
		comment string
	}{{7, 5, "El mejor café del Centro. Ana se acuerda de cómo lo tomas."}, {8, 5, "Tostado ahí mismo, se nota en cada taza."}, {9, 4, "Rico y tranquilo para trabajar un rato."}} {
		var u models.User
		if err := db.Where("email = ?", fmt.Sprintf("demo%02d@empre.app", v.n)).First(&u).Error; err != nil {
			continue
		}
		if err := redeemAndReview(db, u, e, old, v.rating, v.comment, now.AddDate(0, 0, -11+i)); err != nil {
			log.Fatal(err)
		}
	}
	return "Café Ribera: negocio sin oferta, con 3 reseñas verificadas, un servicio de 30 min y agenda"
}

// redeemAndReview crea un canje de este mes (CANJEADO, confirmado por el cliente) y la reseña que respalda.
func redeemAndReview(db *gorm.DB, user models.User, e models.Entity, o models.Offer, rating int, comment string, at time.Time) error {
	// El canje cae en este mes (para «Este mes ahorraste») aunque la reseña diga «hace unos días».
	if y, m, _ := at.Date(); (y != time.Now().Year() || m != time.Now().Month()) && o.EndsAt.After(time.Now()) {
		at = time.Now().Add(-time.Hour)
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return err
	}
	claim := models.OfferClaim{
		ID: uuid.New(), OfferID: o.ID, UserID: user.ID, EntityID: e.ID, Status: models.ClaimRedeemed,
		Token: hex.EncodeToString(tok), ShortCode: strings.ToUpper(hex.EncodeToString(tok[:3])),
		ClaimedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), RedeemedAt: &at, RedeemedBy: &e.OwnerID,
		RedeemedDevice: "seedbarrio", VerifiedByClient: true,
	}
	if err := db.Create(&claim).Error; err != nil {
		return err
	}
	return db.Create(&models.Review{ID: uuid.New(), EntityID: e.ID, UserID: user.ID, Rating: rating, Comment: comment, ClaimID: &claim.ID,
		CreatedAt: at, UpdatedAt: at}).Error
}
