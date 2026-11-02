package services

import (
	"errors"
	"testing"
	"time"

	"empre_backend/internal/database"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// sectionsFixture: un minimercado con su subcategoría y el catálogo de secciones sembrado.
func sectionsFixture(t *testing.T) (*gorm.DB, models.User, models.User, models.Entity, models.Subcategory) {
	t.Helper()
	db := testDB(t)
	owner := models.User{Name: "Rosa", Email: "rosa@x.co", PasswordHash: "x"}
	other := models.User{Name: "Otro", Email: "otro@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Tiendas y comercio"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	sub := models.Subcategory{Name: "Tienda de barrio y minimercado", CategoryID: cat.ID}
	fruver := models.Subcategory{Name: "Fruver", CategoryID: cat.ID}
	for _, s := range []*models.Subcategory{&sub, &fruver} {
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SeedSections(db); err != nil {
		t.Fatal(err)
	}
	// Dos veces: es idempotente (no duplica secciones ni plantillas).
	if err := database.SeedSections(db); err != nil {
		t.Fatal(err)
	}
	ent := models.Entity{OwnerID: owner.ID, Name: "Tienda Sagrado Corazón", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category", "Subcategories").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO entity_subcategories (entity_id, subcategory_id) VALUES (?, ?)", ent.ID, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	return db, owner, other, ent, sub
}

func sectionNames(v []SectionView) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = s.Name
	}
	return out
}

func sameNames(t *testing.T, what string, got []SectionView, want ...string) {
	t.Helper()
	names := sectionNames(got)
	if len(names) != len(want) {
		t.Fatalf("%s: %v, quería %v", what, names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("%s: %v, quería %v", what, names, want)
		}
	}
}

func findSection(t *testing.T, db *gorm.DB, kind, name string) models.Section {
	t.Helper()
	var s models.Section
	if err := db.Where("kind = ? AND name = ?", kind, name).First(&s).Error; err != nil {
		t.Fatalf("no está la sección %s/%s: %v", kind, name, err)
	}
	return s
}

func TestSectionsTemplateAndOrder(t *testing.T) {
	db, owner, other, ent, _ := sectionsFixture(t)
	svc := NewSectionService(db)

	var n int64
	db.Model(&models.Section{}).Where("kind = ? AND name = ?", models.SectionSell, "Bebidas").Count(&n)
	if n != 1 {
		t.Fatalf("cada sección existe una sola vez por tipo: %d", n)
	}

	sell, err := svc.ForEntity(owner.ID, ent.ID, models.SectionSell)
	if err != nil {
		t.Fatal(err)
	}
	sameNames(t, "lo que vendes", sell, "Granos y abarrotes", "Bebidas", "Lácteos y huevos", "Carnes y pollo", "Snacks y dulces", "Aseo del hogar", "Cuidado personal", "Otros")
	supply, err := svc.ForEntity(owner.ID, ent.ID, models.SectionSupply)
	if err != nil {
		t.Fatal(err)
	}
	sameNames(t, "insumos", supply, "Desechables y empaques", "Aseo del local", "Otros")

	if _, err := svc.ForEntity(other.ID, ent.ID, models.SectionSell); err != ErrCatalogNotOwner {
		t.Fatalf("solo el dueño ve sus secciones: %v", err)
	}
	if _, err := svc.ForEntity(owner.ID, ent.ID, "cosa"); !isInvalid(err) {
		t.Fatalf("tipo inválido: %v", err)
	}

	// Un negocio sin subcategoría toma las plantillas de su categoría (las más repetidas primero).
	bare := models.Entity{OwnerID: owner.ID, Name: "Sin subcategoría", CategoryID: ent.CategoryID}
	if err := db.Omit("Owner", "Category", "Subcategories").Create(&bare).Error; err != nil {
		t.Fatal(err)
	}
	fb, err := svc.ForEntity(owner.ID, bare.ID, models.SectionSell)
	if err != nil || len(fb) < 2 || fb[len(fb)-1].Name != models.SectionOther || len(fb) > maxFallbackSections+1 {
		t.Fatalf("respaldo por categoría: %v %v", err, sectionNames(fb))
	}
}

func TestSectionsOnItemsAndPublicProfile(t *testing.T) {
	db, owner, _, ent, _ := sectionsFixture(t)
	svc := NewSectionService(db)
	cat := NewCatalogService(db)
	now := time.Now()
	bebidas := findSection(t, db, models.SectionSell, "Bebidas")
	granos := findSection(t, db, models.SectionSell, "Granos y abarrotes")
	// Una sección del catálogo que no está en su plantilla (es de ferretería).
	herramientas := findSection(t, db, models.SectionSell, "Herramientas")

	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Gaseosa", Kind: "product", Price: intp(2000), SectionID: &bebidas.ID}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Agua", Kind: "product", Price: intp(1500), SectionID: &bebidas.ID}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Azúcar", Kind: "product", Price: intp(5000), SectionID: &granos.ID}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Martillo", Kind: "product", Price: intp(15000), SectionID: &herramientas.ID}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Frito", Kind: "product", Price: intp(1000)}, now); err != nil {
		t.Fatal(err)
	}

	// Usar una sección fuera de la plantilla la agrega a las del negocio (antes de «Otros»).
	sell, _ := svc.ForEntity(owner.ID, ent.ID, models.SectionSell)
	if n := len(sell); n < 2 || sell[n-2].Name != "Herramientas" || sell[n-1].Name != models.SectionOther {
		t.Fatalf("la sección usada queda entre las suyas: %v", sectionNames(sell))
	}
	if sell[1].Name != "Bebidas" || sell[1].Count != 2 {
		t.Fatalf("conteo por sección: %+v", sell[1])
	}

	// El perfil público: solo las secciones con productos, en el orden del negocio, «Otros» al final.
	pub, err := svc.PublicSections(ent.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameNames(t, "perfil", pub, "Granos y abarrotes", "Bebidas", "Herramientas", "Otros")
	if pub[1].Count != 2 || pub[3].Count != 1 {
		t.Fatalf("conteos del perfil: %+v", pub)
	}
}

func TestSectionRequests(t *testing.T) {
	db, owner, other, ent, _ := sectionsFixture(t)
	svc := NewSectionService(db)

	// Pedir una que ya existe (con otra escritura) la agrega de una vez.
	req, sec, err := svc.Request(owner.ID, ent.ID, models.SectionSell, "  herramientas ")
	if err != nil || sec == nil || sec.Name != "Herramientas" || req.ID != uuid.Nil {
		t.Fatalf("pedir una existente la agrega: %v %+v %+v", err, sec, req)
	}
	// Una nueva abre la petición; pedirla otra vez no la duplica.
	req, sec, err = svc.Request(owner.ID, ent.ID, models.SectionSell, "vinos")
	if err != nil || sec != nil || req.Status != models.SectionRequestOpen || req.Name != "Vinos" {
		t.Fatalf("petición nueva: %v %+v", err, req)
	}
	again, _, err := svc.Request(owner.ID, ent.ID, models.SectionSell, "Vinos")
	if err != nil || again.ID != req.ID {
		t.Fatalf("no se duplica: %v", err)
	}
	if _, _, err := svc.Request(other.ID, ent.ID, models.SectionSell, "Otra"); err != ErrCatalogNotOwner {
		t.Fatalf("solo el dueño pide: %v", err)
	}
	if _, _, err := svc.Request(owner.ID, ent.ID, models.SectionSell, "x"); !isInvalid(err) {
		t.Fatalf("nombre corto: %v", err)
	}

	open, err := svc.ListRequests("")
	if err != nil || len(open) != 1 || open[0].EntityName != ent.Name {
		t.Fatalf("el admin ve la petición con el negocio: %v %+v", err, open)
	}
	// El admin la agrega con otro nombre: queda en el catálogo y puesta en el negocio.
	done, err := svc.ResolveRequest(req.ID, true, "Vinos y licores")
	if err != nil || done.Status != models.SectionRequestAdded || done.SectionID == nil {
		t.Fatalf("resolver: %v %+v", err, done)
	}
	if _, err := svc.ResolveRequest(req.ID, false, ""); !isInvalid(err) {
		t.Fatalf("no se resuelve dos veces: %v", err)
	}
	sell, _ := svc.ForEntity(owner.ID, ent.ID, models.SectionSell)
	found := false
	for _, s := range sell {
		found = found || s.Name == "Vinos y licores"
	}
	if !found {
		t.Fatalf("la sección nueva queda en el negocio: %v", sectionNames(sell))
	}
	// Descartar.
	r2, _, _ := svc.Request(owner.ID, ent.ID, models.SectionSupply, "Cajas de cartón")
	if d, err := svc.ResolveRequest(r2.ID, false, ""); err != nil || d.Status != models.SectionRequestDismissed {
		t.Fatalf("descartar: %v %+v", err, d)
	}
	if list, _ := svc.ListRequests(""); len(list) != 0 {
		t.Fatalf("no quedan abiertas: %+v", list)
	}
}

func TestSupplySections(t *testing.T) {
	db, owner, _, ent, _ := sectionsFixture(t)
	led := NewLedgerService(db)
	desechables := findSection(t, db, models.SectionSupply, "Desechables y empaques")
	aseo := findSection(t, db, models.SectionSupply, "Aseo del local")
	bebidasVenta := findSection(t, db, models.SectionSell, "Bebidas")

	bolsas, err := led.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Bolsas", SectionID: &desechables.ID})
	if err != nil || bolsas.Group != "Desechables y empaques" || bolsas.SectionID == nil || *bolsas.SectionID != desechables.ID {
		t.Fatalf("insumo con sección: %v %+v", err, bolsas)
	}
	sinSeccion, err := led.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Escoba"})
	if err != nil || sinSeccion.Group != models.SectionOther {
		t.Fatalf("sin sección va en Otros: %v %q", err, sinSeccion.Group)
	}
	if _, err := led.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Vasos", SectionID: &bebidasVenta.ID}); !isInvalid(err) {
		t.Fatalf("una sección de venta no sirve para un insumo: %v", err)
	}

	ov, err := led.UpdateStockSettings(owner.ID, ent.ID, sinSeccion.ID, ItemSettings{SectionID: &aseo.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range ov.Items {
		if it.Ref == sinSeccion.ID && (it.Group != "Aseo del local" || it.SectionID == nil || *it.SectionID != aseo.ID) {
			t.Fatalf("cambiar la sección del insumo: %+v", it)
		}
	}
	counts, _ := NewSectionService(db).ForEntity(owner.ID, ent.ID, models.SectionSupply)
	if counts[0].Name != "Desechables y empaques" || counts[0].Count != 1 || counts[1].Count != 1 {
		t.Fatalf("conteo de insumos por sección: %+v", counts)
	}
}

func TestResetCatalogWipesBusinessData(t *testing.T) {
	db, owner, _, ent, _ := sectionsFixture(t)
	bebidas := findSection(t, db, models.SectionSell, "Bebidas")
	if _, err := NewCatalogService(db).Create(ent.ID, owner.ID, ServiceInput{Name: "Gaseosa", Kind: "product", SectionID: &bebidas.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLedgerService(db).CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Bolsas"}); err != nil {
		t.Fatal(err)
	}
	if err := database.ResetCatalog(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"entities", "service_items", "stock_items", "entity_subcategories", "subcategory_sections", "subcategories", "categories"} {
		var n int64
		db.Table(table).Count(&n)
		if n != 0 {
			t.Fatalf("%s debía quedar vacía: %d", table, n)
		}
	}
	var users, sections int64
	db.Model(&models.User{}).Count(&users)
	db.Model(&models.Section{}).Count(&sections)
	if users != 2 || sections == 0 {
		t.Fatalf("las cuentas y el catálogo de secciones se quedan: %d usuarios, %d secciones", users, sections)
	}
}

func TestAudienceAndClothingTemplate(t *testing.T) {
	db, owner, _, ent, sub := sectionsFixture(t)
	ropa := models.Subcategory{Name: "Ropa y calzado", CategoryID: sub.CategoryID}
	if err := db.Create(&ropa).Error; err != nil {
		t.Fatal(err)
	}
	// Una sección vieja que nadie usa sale del catálogo al sembrar; una que un ítem usa se queda.
	viejaLibre := models.Section{Kind: models.SectionSell, Name: "Hombre"}
	viejaUsada := models.Section{Kind: models.SectionSell, Name: "Mujer"}
	for _, s := range []*models.Section{&viejaLibre, &viejaUsada} {
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
	}
	tienda := models.Entity{OwnerID: owner.ID, Name: "Moda Caribe", CategoryID: sub.CategoryID}
	if err := db.Omit("Owner", "Category", "Subcategories").Create(&tienda).Error; err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO entity_subcategories (entity_id, subcategory_id) VALUES (?, ?)", tienda.ID, ropa.ID)
	cat := NewCatalogService(db)
	now := time.Now()
	vieja, err := cat.Create(tienda.ID, owner.ID, ServiceInput{Name: "Blusa", Kind: "product", SectionID: &viejaUsada.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SeedSections(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Section{}).Where("id = ?", viejaLibre.ID).Count(&n)
	if n != 0 {
		t.Fatal("una sección sin uso debía salir del catálogo")
	}
	db.Model(&models.Section{}).Where("id = ?", viejaUsada.ID).Count(&n)
	if n != 1 {
		t.Fatal("una sección que un producto usa se queda")
	}

	sell, err := NewSectionService(db).ForEntity(owner.ID, tienda.ID, models.SectionSell)
	if err != nil {
		t.Fatal(err)
	}
	if sell[0].Name != "Camisetas" || sell[2].Name != "Pantalones" {
		t.Fatalf("ropa va por tipo de prenda: %v", sectionNames(sell))
	}

	if !AudienceEnabled(db, tienda.ID) || AudienceEnabled(db, ent.ID) {
		t.Fatal("«para quién» solo en ropa, calzado y deporte")
	}
	camisetas := findSection(t, db, models.SectionSell, "Camisetas")
	aud := []string{"women", "men", "men"}
	polo, err := cat.Create(tienda.ID, owner.ID, ServiceInput{Name: "Camiseta polo", Kind: "product", SectionID: &camisetas.ID, Audience: &aud}, now)
	if err != nil || len(polo.Audience) != 2 || polo.Audience[0] != "men" || polo.Audience[1] != "women" {
		t.Fatalf("para quién se limpia y ordena: %v %v", err, polo.Audience)
	}
	bad := []string{"señores"}
	if _, err := cat.Create(tienda.ID, owner.ID, ServiceInput{Name: "Raro", SectionID: &camisetas.ID, Audience: &bad}, now); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("valor desconocido: %v", err)
	}
	talla, err := cat.Create(tienda.ID, owner.ID, ServiceInput{ParentID: &polo.ID, VariantLabel: "Talla M"}, now)
	if err != nil || len(talla.Audience) != 2 || talla.SectionID == nil || *talla.SectionID != camisetas.ID {
		t.Fatalf("la variante lleva sección y público del producto: %v %+v", err, talla)
	}
	kids := []string{"kids"}
	if _, err := cat.Update(tienda.ID, polo.ID, owner.ID, ServiceInput{Name: "Camiseta polo", Audience: &kids}, now); err != nil {
		t.Fatal(err)
	}
	var tv models.ServiceItem
	db.First(&tv, "id = ?", talla.ID)
	if len(tv.Audience) != 1 || tv.Audience[0] != "kids" {
		t.Fatalf("la variante sigue al producto: %v", tv.Audience)
	}
	// Editar sin mandar «para quién» no lo borra.
	if _, err := cat.Update(tienda.ID, polo.ID, owner.ID, ServiceInput{Name: "Camiseta polo"}, now); err != nil {
		t.Fatal(err)
	}
	var pv models.ServiceItem
	db.First(&pv, "id = ?", polo.ID)
	if len(pv.Audience) != 1 {
		t.Fatalf("editar sin el campo lo conserva: %v", pv.Audience)
	}
	_ = vieja
}

func TestItemPhotos(t *testing.T) {
	db, owner, other, ent, _ := sectionsFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	media := make([]uuid.UUID, 4)
	for i := range media {
		m := models.Media{S3Key: "uploads/x.jpg", ContentType: "image/jpeg"}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
		media[i] = m.ID
	}
	camisa, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Camisa", Kind: "product"}, now)
	if err != nil {
		t.Fatal(err)
	}
	// Orden tal cual llega (la primera es la portada) y sin repetir.
	it, err := cat.SetPhotos(ent.ID, camisa.ID, owner.ID, []uuid.UUID{media[1], media[0], media[1]})
	if err != nil || len(it.Photos) != 2 || it.Photos[0] != media[1].String() {
		t.Fatalf("fotos en orden: %v %v", err, it)
	}
	var stored models.ServiceItem
	db.First(&stored, "id = ?", camisa.ID)
	if len(stored.Photos) != 2 || stored.Photos[1] != media[0].String() {
		t.Fatalf("quedan guardadas: %v", stored.Photos)
	}
	if _, err := cat.SetPhotos(ent.ID, camisa.ID, owner.ID, media); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("máximo 3: %v", err)
	}
	if _, err := cat.SetPhotos(ent.ID, camisa.ID, owner.ID, []uuid.UUID{uuid.New()}); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("una foto que no existe: %v", err)
	}
	if _, err := cat.SetPhotos(ent.ID, camisa.ID, other.ID, media[:1]); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("solo el dueño: %v", err)
	}
	talla, _ := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &camisa.ID, VariantLabel: "M"}, now)
	if _, err := cat.SetPhotos(ent.ID, talla.ID, owner.ID, media[:1]); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("las variantes usan las del producto: %v", err)
	}
	// Quitar todas.
	if it, err := cat.SetPhotos(ent.ID, camisa.ID, owner.ID, nil); err != nil || len(it.Photos) != 0 {
		t.Fatalf("quitar: %v %v", err, it)
	}
	// Íconos: cada sección trae el suyo.
	var sec models.Section
	db.Where("kind = ? AND name = ?", models.SectionSell, "Bebidas").First(&sec)
	if sec.Icon == "" {
		t.Fatal("la sección debía tener ícono")
	}
	views, _ := NewSectionService(db).ForEntity(owner.ID, ent.ID, models.SectionSupply)
	for _, v := range views {
		if v.Icon == "" {
			t.Fatalf("sin ícono: %+v", v)
		}
	}
}
