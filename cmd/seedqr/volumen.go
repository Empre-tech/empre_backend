package main

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"math"
	"math/rand"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// volItem es algo que vende un negocio del volumen.
type volItem struct {
	name, section string
	price         int
}

// volKind es un tipo de negocio: de ahí salen nombres, catálogo, fotos y ofertas.
type volKind struct {
	category string
	names    []string
	tagline  string
	hours    string
	motif    motif
	top, bot color.RGBA
	items    []volItem
	offers   []string
}

var volZones = []struct {
	name     string
	lat, lng float64
	spread   float64 // metros alrededor del centro
	weight   int
}{
	{"Centro", 11.2420, -74.2110, 650, 5},
	{"El Rodadero", 11.2050, -74.2270, 700, 4},
	{"Taganga", 11.2670, -74.1900, 350, 1},
	{"Bastidas", 11.2460, -74.1990, 500, 2},
	{"Pescaíto", 11.2520, -74.2120, 400, 1},
	{"Los Almendros", 11.2300, -74.1940, 500, 2},
	{"Mamatoco", 11.2360, -74.1700, 500, 2},
	{"Gaira", 11.1870, -74.2190, 500, 2},
	{"Pozos Colorados", 11.1560, -74.2290, 700, 1},
}

var volKinds = []volKind{
	{"Corrientazo y almuerzos", []string{"Sazón de la Abuela", "El Corrientazo de Doña Mery", "La Olla Samaria", "Almuerzos La 22", "Fogón Costeño", "Donde Yoli"}, "Almuerzo casero con sopa y jugo", "11:00-15:30", motifPlate, color.RGBA{198, 120, 52, 255}, color.RGBA{96, 48, 22, 255},
		[]volItem{{"Corrientazo del día", "Almuerzos", 15000}, {"Bandeja de pescado", "Almuerzos", 22000}, {"Sopa del día", "Entradas", 7000}, {"Jugo natural", "Bebidas", 5000}}, []string{"Corrientazo con jugo", "Bandeja de pescado con postre"}},
	{"Comida típica", []string{"Casa del Pargo", "La Cazuela Caribe", "Arepas de la 5ta", "El Rincón Samario", "Sancocho y Más"}, "Lo de siempre, como en casa", "10:00-21:00", motifPlate, color.RGBA{176, 88, 44, 255}, color.RGBA{80, 36, 20, 255},
		[]volItem{{"Mojarra frita", "Platos", 32000}, {"Arroz con coco", "Acompañantes", 8000}, {"Patacones", "Acompañantes", 7000}, {"Limonada de coco", "Bebidas", 9000}}, []string{"Mojarra con arroz de coco", "Dos limonadas de coco"}},
	{"Comida rápida", []string{"Perros del Parque", "Burger Bahía", "La Picada 19", "Empanadas Doña Luz", "Salchipapas Camellón", "Pizza Mar Caribe"}, "Rápido, caliente y hasta tarde", "16:00-23:30", motifPlate, color.RGBA{214, 86, 44, 255}, color.RGBA{110, 34, 20, 255},
		[]volItem{{"Hamburguesa sencilla", "Hamburguesas", 16000}, {"Perro especial", "Perros", 12000}, {"Salchipapa", "Papas", 14000}, {"Gaseosa", "Bebidas", 4000}}, []string{"Dos hamburguesas con gaseosa", "Perro y gaseosa"}},
	{"Cafetería", []string{"Café de la Sierra", "Tinto y Punto", "La Molienda", "Café Bolívar", "Grano Tayrona"}, "Café de la Sierra Nevada", "07:00-19:00", motifCup, color.RGBA{136, 92, 60, 255}, color.RGBA{58, 36, 24, 255},
		[]volItem{{"Capuchino", "Bebidas calientes", 8000}, {"Tinto", "Bebidas calientes", 3000}, {"Pandebono", "Panadería", 4000}, {"Torta de banano", "Postres", 9000}}, []string{"2 × 1 en capuchino", "Tinto con pandebono"}},
	{"Heladería", []string{"Helados del Mar", "Paleta Fría", "Coco Loco", "La Nevería Samaria"}, "Para pasar el calor", "11:00-22:00", motifCup, color.RGBA{240, 158, 180, 255}, color.RGBA{150, 70, 110, 255},
		[]volItem{{"Copa de dos bolas", "Helados", 9000}, {"Paleta de coco", "Paletas", 5000}, {"Malteada", "Bebidas", 12000}}, []string{"Segunda copa a mitad", "Malteada con paleta"}},
	{"Barbería", []string{"Barbería El Prado Norte", "Corte Fino", "El Navajazo", "Barber Caribe", "La Barba Samaria", "Estilo 19"}, "Cortes clásicos y degradados", "09:00-20:00", motifPole, color.RGBA{44, 66, 110, 255}, color.RGBA{18, 28, 56, 255},
		[]volItem{{"Corte clásico", "Cortes", 20000}, {"Degradado", "Cortes", 25000}, {"Arreglo de barba", "Barba", 18000}, {"Cera para cabello", "Productos", 18000}}, []string{"Corte + barba", "Degradado con cera"}},
	{"Belleza y peluquería", []string{"Salón Divina", "Uñas y Más", "Peluquería Marina", "Glamour Rodadero", "Studio Bella"}, "Tu cita, sin esperas", "09:00-19:00", motifPole, color.RGBA{170, 72, 110, 255}, color.RGBA{82, 28, 58, 255},
		[]volItem{{"Manicure", "Uñas", 25000}, {"Pedicure", "Uñas", 30000}, {"Tinte", "Cabello", 70000}, {"Cepillado", "Cabello", 28000}}, []string{"Manicure y pedicure", "Cepillado con hidratación"}},
	{"Ropa y calzado", []string{"Almacén La Bahía", "Lino y Sol", "Moda Samaria", "Calzado Tayrona", "Boutique Rodadero", "Sandalias del Mar"}, "Ropa fresca para el calor", "09:30-19:30", motifPhone, color.RGBA{72, 120, 140, 255}, color.RGBA{26, 54, 70, 255},
		[]volItem{{"Camisa de lino", "Hombre", 65000}, {"Vestido de playa", "Mujer", 85000}, {"Sandalias", "Calzado", 45000}, {"Sombrero", "Accesorios", 35000}}, []string{"Camisa de lino con descuento", "Segundas sandalias a mitad"}},
	{"Droguería", []string{"Droguería La Salud", "Farma Caribe", "Droguería 24 Horas", "Medic Plus"}, "Abierto cuando lo necesitas", "", motifPhone, color.RGBA{58, 148, 138, 255}, color.RGBA{18, 78, 72, 255},
		[]volItem{{"Bloqueador solar", "Cuidado personal", 42000}, {"Suero oral", "Medicamentos", 6000}, {"Toma de presión", "Servicios", 5000}}, []string{"Bloqueador con descuento"}},
	{"Ferretería", []string{"Ferretería El Tornillo", "Materiales La 30", "Todo Obra"}, "Lo que falta para terminar hoy", "07:30-18:00", motifPhone, color.RGBA{190, 140, 56, 255}, color.RGBA{86, 60, 22, 255},
		[]volItem{{"Galón de pintura", "Pinturas", 62000}, {"Caja de tornillos", "Herrajes", 9000}, {"Cinta aislante", "Eléctricos", 4000}}, []string{"Pintura con rodillo gratis"}},
	{"Celulares y accesorios", []string{"Cell Caribe", "Reparo Tu Cel", "Mundo Móvil", "Accesorios 22"}, "Te lo arreglamos hoy", "09:00-19:00", motifPhone, color.RGBA{70, 80, 150, 255}, color.RGBA{26, 30, 74, 255},
		[]volItem{{"Cambio de pantalla", "Reparación", 120000}, {"Vidrio templado", "Accesorios", 15000}, {"Cargador rápido", "Accesorios", 35000}}, []string{"Vidrio templado con forro"}},
	{"Gimnasio y crossfit", []string{"Fit Samaria", "Box Caribe", "Gym La Bahía"}, "Primera clase sin compromiso", "05:30-21:00", motifPhone, color.RGBA{60, 60, 66, 255}, color.RGBA{20, 20, 24, 255},
		[]volItem{{"Mensualidad", "Planes", 90000}, {"Día de entrenamiento", "Planes", 12000}, {"Clase de crossfit", "Clases", 15000}}, []string{"Semana de prueba", "Primer mes con descuento"}},
	{"Guardería y paseos", []string{"Patitas Felices", "Paseos Doña Rita", "Can Caribe"}, "Tu mascota, bien cuidada", "07:00-18:00", motifPaw, color.RGBA{150, 110, 70, 255}, color.RGBA{72, 48, 26, 255},
		[]volItem{{"Día de guardería", "Guardería", 35000}, {"Paseo de una hora", "Paseos", 15000}, {"Baño", "Aseo", 30000}}, []string{"Baño con paseo"}},
	{"Bar", []string{"La Terraza del Mar", "Bar Camellón", "Noche Samaria", "El Muelle"}, "Buena música y frente al mar", "17:00-02:00", motifCup, color.RGBA{86, 50, 110, 255}, color.RGBA{30, 16, 44, 255},
		[]volItem{{"Mojito", "Cócteles", 22000}, {"Cerveza artesanal", "Cervezas", 12000}, {"Picada", "Para compartir", 38000}}, []string{"2 × 1 en mojitos", "Cerveza con picada"}},
	{"Hostal", []string{"Hostal Sierra Azul", "Casa Mar", "Posada Taganga"}, "A pasos del mar", "", motifPhone, color.RGBA{60, 130, 170, 255}, color.RGBA{20, 62, 90, 255},
		[]volItem{{"Cama en dormitorio", "Alojamiento", 55000}, {"Habitación privada", "Alojamiento", 140000}}, []string{"Tercera noche a mitad"}},
}

const volEmail = "vol%03d@empre.app"

var volOwners = []string{"Rosa", "Luis", "Carmen", "Jorge", "Yolanda", "Rafael", "Marina", "Efraín", "Nubia", "Álvaro", "Dayana", "Wilmer", "Ingrid", "Omar", "Katherine"}
var volSurnames = []string{"Barros", "Pertuz", "Mejía", "Fontalvo", "De la Hoz", "Ospino", "Pinedo", "Orozco", "Cantillo", "Vives"}
var volReviews = []string{
	"Me atendieron rápido y sin vueltas.", "Buen precio, volvería.", "El sitio limpio y la atención muy amable.",
	"Tal como lo publican en Empre.", "Un poco de fila, pero vale la pena.", "Me explicaron todo con paciencia.",
}

// seedVolumen crea n negocios en Santa Marta, cada uno con dueño propio (vol001@empre.app…). Es repetible: borra solo
// los negocios de esas cuentas y los vuelve a crear.
func seedVolumen(db *gorm.DB, n int, store uploader, reviewers []models.User) {
	r := rand.New(rand.NewSource(20261104))
	zones := services.NewZoneService(db)
	now := utils.NowInCartagena()

	var cats []models.Category
	if err := db.Find(&cats).Error; err != nil {
		log.Fatal(err)
	}
	catID := map[string]uuid.UUID{}
	for _, c := range cats {
		catID[c.Name] = c.ID
	}
	kinds := make([]volKind, 0, len(volKinds))
	for _, k := range volKinds {
		if _, ok := catID[k.category]; ok {
			kinds = append(kinds, k)
		} else {
			fmt.Println("Sin la categoría, se salta:", k.category)
		}
	}
	if len(kinds) == 0 {
		log.Fatal("No hay categorías para sembrar")
	}

	// Las fotos dibujadas se suben una vez por tipo y se reutilizan.
	photos := map[string][]uuid.UUID{}
	if store != nil {
		for _, k := range kinds {
			for v := 0; v < 3; v++ {
				data := samplePhoto(k.top, k.bot, k.motif, v+1)
				key := fmt.Sprintf("seed/vol/%s.jpg", uuid.NewString())
				if err := store.UploadFile(key, bytes.NewReader(data), "image/jpeg"); err != nil {
					log.Fatal("No se pudo subir la foto: ", err)
				}
				media := models.Media{S3Key: key, OriginalName: "ejemplo.jpg", ContentType: "image/jpeg", Size: int64(len(data))}
				if err := db.Create(&media).Error; err != nil {
					log.Fatal(err)
				}
				photos[k.category] = append(photos[k.category], media.ID)
			}
		}
		fmt.Println("Fotos de ejemplo subidas.")
	}

	totalWeight := 0
	for _, z := range volZones {
		totalWeight += z.weight
	}
	nameUse := map[string]int{}
	offers, reviews := 0, 0

	for i := 0; i < n; i++ {
		k := kinds[i%len(kinds)]
		base := k.names[nameUse[k.category]%len(k.names)]
		turn := nameUse[k.category] / len(k.names)
		nameUse[k.category]++

		pick := r.Intn(totalWeight)
		zone := volZones[0]
		for _, z := range volZones {
			if pick < z.weight {
				zone = z
				break
			}
			pick -= z.weight
		}
		name := base
		if turn > 0 {
			name = fmt.Sprintf("%s %s", base, zone.name)
		}
		ang, dist := r.Float64()*2*math.Pi, math.Sqrt(r.Float64())*zone.spread
		lat, lng := offset(zone.lat, zone.lng, dist*math.Sin(ang), dist*math.Cos(ang))
		ownerName := volOwners[r.Intn(len(volOwners))] + " " + volSurnames[r.Intn(len(volSurnames))]
		owner := ensureUser(db, fmt.Sprintf(volEmail, i+1), ownerName)

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := clearOwner(tx, owner.ID); err != nil {
				return err
			}
			e := models.Entity{
				ID: uuid.New(), OwnerID: owner.ID, Name: name, Tagline: k.tagline,
				Description:        fmt.Sprintf("%s en %s, Santa Marta. Atiende %s.", k.tagline, zone.name, ownerName),
				AttendantName:      volOwners[r.Intn(len(volOwners))],
				CategoryID:         catID[k.category],
				City:               "Santa Marta",
				Address:            fmt.Sprintf("Calle %d #%d-%02d", 5+r.Intn(30), 1+r.Intn(25), 10+r.Intn(80)),
				ContactInfo:        fmt.Sprintf("+57 300 55%d %04d", r.Intn(10), i+1),
				Latitude:           lat,
				Longitude:          lng,
				VerificationStatus: models.StatusVerified,
				IsVerified:         true,
				ServiceMode:        models.ServiceModeInPlace,
			}
			if err := zones.Assign(&e); err != nil {
				return err
			}
			if err := tx.Create(&e).Error; err != nil {
				return err
			}
			if err := tx.Create(hoursFor(e.ID, k.hours)).Error; err != nil {
				return err
			}
			stamp := now
			items := make([]models.ServiceItem, 0, len(k.items))
			for _, it := range k.items {
				price := it.price + r.Intn(4)*1000
				items = append(items, models.ServiceItem{EntityID: e.ID, Name: it.name, Section: it.section, Price: &price, PriceUpdatedAt: &stamp, ConfirmedAt: &stamp})
			}
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
			// Casi la mitad tiene oferta, con finales repartidos: unas a punto de acabar y otras de varios días.
			if r.Intn(100) < 45 {
				it := items[r.Intn(len(items))]
				usual := *it.Price
				special := usual * (55 + r.Intn(30)) / 100 / 500 * 500
				ends := []time.Duration{40 * time.Minute, 2 * time.Hour, 6 * time.Hour, 26 * time.Hour, 72 * time.Hour}[r.Intn(5)]
				o := models.Offer{
					ID: uuid.New(), EntityID: e.ID, Type: kindFor(ends), Title: k.offers[r.Intn(len(k.offers))],
					Price: pesos(special), NormalPrice: pesos(usual), SpecialPrice: &special, ReferencePrice: &usual,
					StartsAt: now.Add(-30 * time.Minute), EndsAt: now.Add(ends), TotalSlots: 5 + r.Intn(25),
					RadiusMeters: 3000, RepeatPolicy: models.RepeatOnce,
				}
				if err := tx.Omit("Entity", "Items").Create(&o).Error; err != nil {
					return err
				}
				id := it.ID
				if err := tx.Create(&models.OfferItem{OfferID: o.ID, ServiceItemID: &id, Name: it.Name, Price: &usual}).Error; err != nil {
					return err
				}
				offers++
			}
			if ids := photos[k.category]; len(ids) > 0 {
				post := models.Post{EntityID: e.ID, Caption: k.tagline, CreatedAt: now.Add(-time.Duration(r.Intn(240)) * time.Hour), UpdatedAt: now}
				if err := tx.Omit("Photos").Create(&post).Error; err != nil {
					return err
				}
				for j := 0; j < len(ids); j++ {
					if err := tx.Omit("Media", "Post").Create(&models.EntityPhoto{PostID: post.ID, EntityID: e.ID, MediaID: ids[(i+j)%len(ids)], Order: j}).Error; err != nil {
						return err
					}
				}
			}
			// Algunos, a propósito, sin reseñas: la pantalla debe verse bien sin ellas.
			for w := 0; w < r.Intn(len(reviewers)+1); w++ {
				at := now.AddDate(0, 0, -r.Intn(60))
				rev := models.Review{EntityID: e.ID, UserID: reviewers[w].ID, Rating: 3 + r.Intn(3), Comment: volReviews[r.Intn(len(volReviews))], CreatedAt: at, UpdatedAt: at}
				if err := tx.Omit("User").Create(&rev).Error; err != nil {
					return err
				}
				reviews++
			}
			return nil
		})
		if err != nil {
			log.Fatalf("Negocio %d (%s): %v", i+1, name, err)
		}
		if (i+1)%25 == 0 {
			fmt.Printf("%d de %d negocios…\n", i+1, n)
		}
	}
	fmt.Printf("Listo: %d negocios en Santa Marta (%d con oferta, %d reseñas). Cuentas vol001..vol%03d@empre.app.\n", n, offers, reviews, n)
}

// borrarVolumen quita los negocios de las cuentas vol*. No toca ninguna otra cuenta.
func borrarVolumen(db *gorm.DB) {
	var users []models.User
	if err := db.Where("email LIKE ?", "vol___@empre.app").Find(&users).Error; err != nil {
		log.Fatal(err)
	}
	for _, u := range users {
		if err := db.Transaction(func(tx *gorm.DB) error { return clearOwner(tx, u.ID) }); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("Quitados los negocios de %d cuentas de volumen.\n", len(users))
}

// kindFor: el tipo de oferta según cuánto dura.
func kindFor(d time.Duration) models.OfferType {
	switch {
	case d < 24*time.Hour:
		return models.OfferHours
	case d < 48*time.Hour:
		return models.OfferDay
	}
	return models.OfferWeek
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
