package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/observability"
	"empre_backend/internal/repository"
	"empre_backend/internal/services"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GET /api/entities (el listado del mapa): fija lo que devuelve y cuántas consultas cuesta, contra Postgres real
// (TEST_DATABASE_URL), para poder reducir consultas sin cambiar la respuesta.
//
//	TEST_DATABASE_URL="host=localhost port=5433 user=postgres password=postgres dbname=postgres sslmode=disable" go test -p 1 ./internal/handlers/
func entityListFixture(t *testing.T) (*gorm.DB, *gin.Engine, map[string]uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no definido")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public")
	db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	if err := db.AutoMigrate(&models.User{}, &models.Category{}, &models.Media{}, &models.Entity{}, &models.Post{}, &models.EntityPhoto{},
		&models.Subcategory{}, &models.BusinessHour{}, &models.Review{}, &models.Subscription{}); err != nil {
		t.Fatal(err)
	}
	if err := observability.InstrumentGorm(db); err != nil {
		t.Fatal(err)
	}

	ids := map[string]uuid.UUID{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner := models.User{Email: "dueno@empre.test", Name: "Dueño"}
	must(db.Create(&owner).Error)
	food := models.Category{Name: "Comida", Icon: "restaurant-outline", Color: "#F58220"}
	gone := models.Category{Name: "Borrada", Icon: "trash-outline"}
	must(db.Create(&food).Error)
	must(db.Create(&gone).Error)
	must(db.Delete(&gone).Error)
	ids["food"], ids["gone"] = food.ID, gone.ID
	profile := models.Media{S3Key: "perfil/arepas.jpg", ContentType: "image/jpeg"}
	banner := models.Media{S3Key: "portada/arepas.jpg", ContentType: "image/jpeg"}
	photo := models.Media{S3Key: "galeria/1.jpg", ContentType: "image/jpeg"}
	must(db.Create(&profile).Error)
	must(db.Create(&banner).Error)
	must(db.Create(&photo).Error)

	near := func(key, name string, cat uuid.UUID, mut func(*models.Entity)) models.Entity {
		e := models.Entity{OwnerID: owner.ID, Name: name, CategoryID: cat, Latitude: 11.2408, Longitude: -74.2110}
		if mut != nil {
			mut(&e)
		}
		must(db.Create(&e).Error)
		ids[key] = e.ID
		return e
	}
	// Completo: perfil, portada, galería, subcategoría, reseñas (una borrada) y abierto 24 h.
	full := near("full", "Arepas Doña Ana", food.ID, func(e *models.Entity) {
		e.ProfileMediaID, e.BannerMediaID, e.IsVerified, e.ServiceMode = &profile.ID, &banner.ID, true, models.ServiceMode("both")
	})
	post := models.Post{EntityID: full.ID}
	must(db.Create(&post).Error)
	must(db.Create(&models.EntityPhoto{PostID: post.ID, EntityID: full.ID, MediaID: photo.ID}).Error)
	sub := models.Subcategory{Name: "Arepas", CategoryID: food.ID}
	must(db.Create(&sub).Error)
	must(db.Model(&full).Association("Subcategories").Append(&sub))
	for d := 0; d < 7; d++ {
		must(db.Create(&models.BusinessHour{EntityID: full.ID, Weekday: d, Is24h: true}).Error)
	}
	for i, r := range []int{5, 3, 1} {
		u := models.User{Email: "cliente" + string(rune('a'+i)) + "@empre.test", Name: "Cliente"}
		must(db.Create(&u).Error)
		rv := models.Review{EntityID: full.ID, UserID: u.ID, Rating: r}
		must(db.Create(&rv).Error)
		if r == 1 {
			must(db.Delete(&rv).Error) // borrada: no cuenta
		}
	}
	// Mínimo: sin foto, sin reseñas y sin horario; cerrado todos los días no aplica.
	plain := near("plain", "Tienda Sin Foto", food.ID, nil)
	// Siempre cerrado (tiene horario pero no abre).
	closed := near("closed", "Panadería Cerrada", food.ID, nil)
	for d := 0; d < 7; d++ {
		must(db.Create(&models.BusinessHour{EntityID: closed.ID, Weekday: d, Closed: true}).Error)
	}
	_ = plain
	// Categoría borrada: lo que haga hoy el listado con ella queda fijado por la prueba.
	near("gonecat", "Negocio De Categoría Borrada", gone.ID, nil)
	// Fuera de la respuesta: lejos, borrado, oculto por la suscripción.
	near("far", "Lejos", food.ID, func(e *models.Entity) { e.Latitude, e.Longitude = 4.7110, -74.0721 })
	del := near("deleted", "Borrado", food.ID, nil)
	must(db.Delete(&del).Error)
	hidden := near("hidden", "Oculto Por Plan", food.ID, nil)
	old := time.Now().AddDate(0, 0, -10)
	must(db.Create(&models.Subscription{EntityID: hidden.ID, Status: models.SubscriptionStatus("trial"), TrialThresholdNotifiedAt: &old}).Error)

	// Firma de S3 sin red: credenciales falsas, la firma es un cálculo local.
	s3c := s3.New(s3.Options{Region: "us-east-2", Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKIDPRUEBA", "secretoprueba", ""))})
	storage := &services.StorageService{S3Client: s3c, PresignClient: s3.NewPresignClient(s3c), Bucket: "empre-prueba"}
	media := services.NewMediaService(repository.NewMediaRepository(db), storage, "")
	h := NewEntityHandler(services.NewEntityService(repository.NewEntityRepository(db), media), media, db,
		services.NewReviewService(repository.NewReviewRepository(db), nil), nil, nil, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/entities", h.FindAll)
	return db, r, ids
}

type listItem struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	CategoryID    uuid.UUID `json:"category_id"`
	CategoryName  string    `json:"category_name"`
	CategoryIcon  string    `json:"category_icon"`
	CategoryColor string    `json:"category_color"`
	ProfileURL    string    `json:"profile_url"`
	IsVerified    bool      `json:"is_verified"`
	AvgRating     float64   `json:"avg_rating"`
	ReviewCount   int64     `json:"review_count"`
	ServiceMode   string    `json:"service_mode"`
	IsOpenNow     bool      `json:"is_open_now"`
	HasHours      bool      `json:"has_hours"`
}

type listBody struct {
	Data []listItem `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
	raw map[string]any
}

// list hace la petición y devuelve la respuesta (ordenada por nombre: el endpoint no fija orden) y cuántas consultas costó.
func list(t *testing.T, r *gin.Engine, query string) (listBody, int64) {
	t.Helper()
	q0, _ := observability.DBTotals()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/entities?"+query, nil))
	q1, _ := observability.DBTotals()
	if w.Code != http.StatusOK {
		t.Fatalf("%s: estado %d: %s", query, w.Code, w.Body.String())
	}
	var b listBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &b.raw)
	sort.Slice(b.Data, func(i, j int) bool { return b.Data[i].Name < b.Data[j].Name })
	return b, q1 - q0
}

func names(b listBody) string {
	out := make([]string, len(b.Data))
	for i, d := range b.Data {
		out[i] = d.Name
	}
	return strings.Join(out, " | ")
}

const nearQ = "lat=11.2408&long=-74.2110&radius=5000&pageSize=50"

func TestEntityListResponse(t *testing.T) {
	_, r, ids := entityListFixture(t)

	// 2 consultas: la página (con total y reseñas) y los horarios. Eran 6–7 en serie, una ida y vuelta cada una.
	b, queries := list(t, r, nearQ)
	if queries != 2 {
		t.Fatalf("consultas por petición: %d, se esperaban 2", queries)
	}
	if got, want := names(b), "Arepas Doña Ana | Negocio De Categoría Borrada | Panadería Cerrada | Tienda Sin Foto"; got != want {
		t.Fatalf("negocios:\n got %s\nwant %s", got, want)
	}
	if b.Meta.Total != 4 {
		t.Fatalf("total %d, se esperaban 4", b.Meta.Total)
	}

	full := b.Data[0]
	if full.ID != ids["full"] || full.CategoryID != ids["food"] || full.CategoryName != "Comida" || full.CategoryIcon != "restaurant-outline" ||
		full.CategoryColor != "#F58220" || !full.IsVerified || full.ServiceMode != "both" {
		t.Fatalf("datos del negocio completo: %+v", full)
	}
	// La foto de perfil sale firmada (la portada y la galería no forman parte de esta respuesta).
	if !strings.Contains(full.ProfileURL, "perfil/arepas.jpg") || !strings.Contains(full.ProfileURL, "X-Amz-Signature=") {
		t.Fatalf("foto de perfil: %q", full.ProfileURL)
	}
	if full.AvgRating != 4 || full.ReviewCount != 2 {
		t.Fatalf("reseñas (la borrada no cuenta): promedio %v, cantidad %d", full.AvgRating, full.ReviewCount)
	}
	if !full.IsOpenNow || !full.HasHours {
		t.Fatalf("abre 24 h: abierto %v, con horario %v", full.IsOpenNow, full.HasHours)
	}

	gone := b.Data[1] // su categoría está borrada: el listado no la trae
	if gone.CategoryID != uuid.Nil || gone.CategoryName != "" {
		t.Fatalf("categoría borrada: %+v", gone)
	}
	closed, plain := b.Data[2], b.Data[3]
	if closed.IsOpenNow || !closed.HasHours {
		t.Fatalf("siempre cerrado: abierto %v, con horario %v", closed.IsOpenNow, closed.HasHours)
	}
	if plain.ProfileURL != "" || plain.AvgRating != 0 || plain.ReviewCount != 0 || plain.HasHours || plain.IsOpenNow {
		t.Fatalf("negocio mínimo: %+v", plain)
	}

	// Las claves del JSON no cambian (la app lee estas).
	keys := []string{}
	for k := range b.raw["data"].([]any)[0].(map[string]any) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "avg_rating,category_color,category_icon,category_id,category_name,has_hours,id,is_open_now,is_verified,latitude,longitude,name,profile_url,review_count,service_mode" {
		t.Fatalf("claves del JSON: %s", got)
	}
}

func TestEntityListFilters(t *testing.T) {
	_, r, ids := entityListFixture(t)
	cases := []struct {
		query, want    string
		total, queries int64
	}{
		{nearQ + "&category=" + ids["food"].String(), "Arepas Doña Ana | Panadería Cerrada | Tienda Sin Foto", 3, 2},
		{nearQ + "&q=arepas", "Arepas Doña Ana", 1, 2},
		{nearQ + "&open_now=true", "Arepas Doña Ana", 1, 2},
		// Sin resultados no hay horarios que precargar: 1 consulta.
		{nearQ + "&q=nadie", "", 0, 1},
		// Página fuera de rango: sin negocios, pero el total sigue siendo el de todo el filtro (se cuenta aparte).
		{nearQ + "&page=3", "", 4, 2},
		// Sin coordenadas: también entran los lejanos.
		{"pageSize=50&q=lejos", "Lejos", 1, 2},
	}
	for _, c := range cases {
		b, queries := list(t, r, c.query)
		if got := names(b); got != c.want || b.Meta.Total != c.total || queries != c.queries {
			t.Fatalf("%s:\n got %q (total %d, %d consultas)\nwant %q (total %d, %d consultas)", c.query, got, b.Meta.Total, queries, c.want, c.total, c.queries)
		}
	}
}
