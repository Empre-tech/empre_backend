// Command seedbarrio prepara los negocios demo de Santa Marta para revisar la pestaña Ofertas del barrio (3.48)
// sin borrar nada: vuelve a poner vigentes las ofertas demo que ya terminaron, les escribe la nota de quien atiende,
// pone el nombre de quien atiende en cada negocio y crea unos pocos canjes de este mes con su reseña verificada
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
		Preload("Owner").Find(&ents).Error; err != nil {
		log.Fatal(err)
	}
	if len(ents) == 0 {
		log.Fatal("No hay negocios demo: corre primero go run ./cmd/seeddemo")
	}
	byName := map[string]models.Entity{}
	reopened := 0
	for i, e := range ents {
		byName[e.Name] = e
		first := strings.Fields(e.Owner.Name)
		if len(first) > 0 && e.AttendantName == "" { // vacío o NULL (columna nueva)
			db.Model(&models.Entity{}).Where("id = ?", e.ID).Update("attendant_name", first[0])
		}
		if n, ok := notes[e.Name]; ok {
			db.Model(&models.Offer{}).Where("entity_id = ? AND coalesce(owner_note, '') = ''", e.ID).Update("owner_note", n)
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
	fmt.Printf("Listo: %d ofertas demo vigentes otra vez, notas y quién atiende en %d negocios, %d canjes con reseña verificada.\n", reopened, len(ents), made)
}

// redeemAndReview crea un canje de este mes (CANJEADO, confirmado por el cliente) y la reseña que respalda.
func redeemAndReview(db *gorm.DB, user models.User, e models.Entity, o models.Offer, rating int, comment string, at time.Time) error {
	// El canje cae en este mes (para «Este mes ahorraste») aunque la reseña diga «hace unos días».
	if y, m, _ := at.Date(); y != time.Now().Year() || m != time.Now().Month() {
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
