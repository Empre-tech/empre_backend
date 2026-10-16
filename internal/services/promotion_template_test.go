package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

func TestValidatePromotionContent(t *testing.T) {
	ok := PromotionContent{Title: "Aniversario", Template: models.TemplateCelebrar}
	bad := map[string]PromotionContent{
		"título corto":            {Title: "a", Template: models.TemplateCelebrar},
		"título largo":            {Title: strings.Repeat("x", 81), Template: models.TemplateCelebrar},
		"mensaje largo":           {Title: "Hola", Template: models.TemplateCelebrar, PublicMessage: strings.Repeat("x", 241)},
		"motivo largo":            {Title: "Hola", Template: models.TemplateCelebrar, MotiveInternal: strings.Repeat("x", 201)},
		"plantilla desconocida":   {Title: "Hola", Template: "arcoiris"},
		"sin plantilla ni precio": {Title: "Hola"},
		"estrella sin servicio":   {Title: "Hola", Template: models.TemplateEstrella},
		"paquete con un servicio": {Title: "Hola", Template: models.TemplatePaquete, ItemCount: 1},
	}
	if err := ValidatePromotionContent(ok); err != nil {
		t.Fatalf("la celebración no pide descuento ni servicios: %v", err)
	}
	for name, c := range bad {
		if err := ValidatePromotionContent(c); !errors.Is(err, ErrPromotionInvalid) {
			t.Errorf("%s: debía dar ErrPromotionInvalid, dio %v", name, err)
		}
	}
	good := []PromotionContent{
		{Title: "Oferta", Price: "20000"}, // sin plantilla, con precio (de antes)
		{Title: "Oferta", Discount: "20"}, // sin plantilla, con descuento
		{Title: "Estrella", Template: models.TemplateEstrella, ItemCount: 1},
		{Title: "Combo", Template: models.TemplatePaquete, ItemCount: 2},
		{Title: strings.Repeat("ñ", 80), Template: models.TemplateCelebrar, PublicMessage: strings.Repeat("ñ", 240), MotiveInternal: strings.Repeat("ñ", 200)},
	}
	for i, c := range good {
		if err := ValidatePromotionContent(c); err != nil {
			t.Errorf("caso válido %d: %v", i, err)
		}
	}
}

func promoService(f *offerFixture) *PromotionService {
	db := f.offers.OfferRepo.DB
	return NewPromotionService(repository.NewPromotionRepository(db), repository.NewEntityRepository(db))
}

func inDays(n int) time.Time { return utils.NowInCartagena().Add(time.Duration(n) * 24 * time.Hour) }

func TestPromotion_DraftPublishAndPrivacy(t *testing.T) {
	f := newOfferFixture(t)
	ps := promoService(f)

	// Borrador: sin fecha de fin, sin consumir el límite y sin verse en público.
	draft, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{
		Title: "Primer aniversario", Template: models.TemplateCelebrar, Draft: true,
		MotiveInternal: "Subir ventas del mes flojo", PublicMessage: "Celebramos un año contigo",
	})
	if err != nil || draft.Status != models.PromotionDraft {
		t.Fatalf("borrador: %v %+v", err, draft)
	}
	if pub, _ := ps.ActiveForEntity(f.shop.ID); len(pub) != 0 {
		t.Fatalf("un borrador no es público: %+v", pub)
	}
	if n, _ := ps.PromotionRepo.CountActiveByEntity(f.shop.ID, utils.NowInCartagena()); n != 0 {
		t.Fatalf("un borrador no cuenta para el límite: %d", n)
	}

	// Publicar el borrador exige fecha de fin futura.
	if _, err := ps.Update(draft.ID, f.owner, PublishPromotionInput{}, "active"); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("publicar sin fecha de fin: %v", err)
	}
	live, err := ps.Update(draft.ID, f.owner, PublishPromotionInput{EndsAt: inDays(7)}, "active")
	if err != nil || live.Status != models.PromotionActive || live.StartsAt.IsZero() {
		t.Fatalf("publicar: %v %+v", err, live)
	}
	pub, _ := ps.ActiveForEntity(f.shop.ID)
	if len(pub) != 1 || pub[0].ID != draft.ID {
		t.Fatalf("ahora sí es pública: %+v", pub)
	}

	// El motivo interno NUNCA sale en una respuesta pública; el dueño sí lo ve.
	public, _ := json.Marshal(dtos.PromotionResponseFrom(&pub[0]))
	if strings.Contains(string(public), "Subir ventas") || strings.Contains(string(public), "motive") {
		t.Fatalf("el motivo interno se filtró a la respuesta pública: %s", public)
	}
	if !strings.Contains(string(public), "Celebramos un año contigo") {
		t.Fatalf("el mensaje público sí debe verse: %s", public)
	}
	owner, _ := json.Marshal(dtos.PromotionOwnerResponseFrom(live))
	if !strings.Contains(string(owner), "Subir ventas del mes flojo") {
		t.Fatalf("el dueño debe ver su motivo: %s", owner)
	}
	// Tampoco sale al serializar el modelo directamente.
	raw, _ := json.Marshal(live)
	if strings.Contains(string(raw), "Subir ventas") {
		t.Fatalf("el modelo no debe serializar el motivo: %s", raw)
	}
}

func TestPromotion_TemplatesServicesAndFreeze(t *testing.T) {
	f := newOfferFixture(t)
	ps := promoService(f)

	// Estrella y paquete exigen sus servicios; la celebración no.
	if _, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Estrella", Template: models.TemplateEstrella, EndsAt: inDays(5)}); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("estrella sin servicio: %v", err)
	}
	if _, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Combo", Template: models.TemplatePaquete, ServiceItemIDs: []uuid.UUID{f.corte.ID}, EndsAt: inDays(5)}); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("paquete con un servicio: %v", err)
	}
	if _, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Combo", Template: models.TemplatePaquete, ServiceItemIDs: []uuid.UUID{f.corte.ID, f.ajeno.ID}, EndsAt: inDays(5)}); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("servicio de otro negocio: %v", err)
	}
	var n int64
	f.offers.OfferRepo.DB.Model(&models.Promotion{}).Where("entity_id = ?", f.shop.ID).Count(&n)
	if n != 0 {
		t.Fatalf("los rechazos no dejan promociones a medias: %d", n)
	}

	combo, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{
		Title: "Corte + barba", Template: models.TemplatePaquete, PublicMessage: "Para llegar impecable",
		ServiceItemIDs: []uuid.UUID{f.corte.ID, f.barba.ID}, EndsAt: inDays(5),
	})
	if err != nil || len(combo.Items) != 2 || combo.Items[0].Name != "Corte clásico" || *combo.Items[1].Price != 12000 {
		t.Fatalf("paquete: %v %+v", err, combo)
	}
	// Sin descuento ni precio: una promoción comunica, no tiene que rebajar.
	if combo.Price != "" || combo.Discount != "" {
		t.Fatalf("el paquete no inventa precio ni descuento: %+v", combo)
	}

	// Cambiar o retirar el servicio del catálogo NO altera la campaña publicada.
	if _, err := f.catalog.Update(f.shop.ID, f.corte.ID, f.owner, ServiceInput{Name: "Corte renombrado", Price: intp(99000)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.catalog.Retire(f.shop.ID, f.barba.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	pub, _ := ps.ActiveForEntity(f.shop.ID)
	if len(pub) != 1 || len(pub[0].Items) != 2 || pub[0].Items[0].Name != "Corte clásico" || *pub[0].Items[0].Price != 25000 || pub[0].Items[1].Name != "Arreglo de barba" {
		t.Fatalf("la copia congelada no cambia con el catálogo: %+v", pub[0].Items)
	}

	// Editar solo el título no toca los servicios; reemplazarlos sí (y se vuelve a congelar).
	kept, err := ps.Update(combo.ID, f.owner, PublishPromotionInput{Title: "Corte + barba de lujo"}, "")
	if err != nil || len(kept.Items) != 2 {
		t.Fatalf("editar el título conserva los servicios: %v %+v", err, kept.Items)
	}
	swapped, err := ps.Update(combo.ID, f.owner, PublishPromotionInput{ServiceItemIDs: []uuid.UUID{f.corte.ID, f.cejas.ID}, ReplaceItems: true}, "")
	if err != nil || len(swapped.Items) != 2 || swapped.Items[0].Name != "Corte renombrado" || *swapped.Items[0].Price != 99000 || swapped.Items[1].Price != nil {
		t.Fatalf("reemplazar vuelve a congelar con lo de hoy: %v %+v", err, swapped.Items)
	}
	// Un paquete no puede quedarse con un solo servicio.
	if _, err := ps.Update(combo.ID, f.owner, PublishPromotionInput{ServiceItemIDs: []uuid.UUID{f.corte.ID}, ReplaceItems: true}, ""); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("paquete con un servicio al editar: %v", err)
	}
	var stored int64
	f.offers.OfferRepo.DB.Model(&models.PromotionItem{}).Where("promotion_id = ?", combo.ID).Count(&stored)
	if stored != 2 {
		t.Fatalf("un cambio rechazado no toca los servicios guardados: %d", stored)
	}

	// Una celebración sin servicios es válida y los conserva si no se piden otros.
	cel, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Aniversario", Template: models.TemplateCelebrar, EndsAt: inDays(3)})
	if err != nil || len(cel.Items) != 0 {
		t.Fatalf("celebración: %v %+v", err, cel)
	}
}

func TestPromotion_PublishingADraftRefreezesCurrentPrices(t *testing.T) {
	f := newOfferFixture(t)
	ps := promoService(f)
	draft, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{
		Title: "Combo de temporada", Template: models.TemplatePaquete, Draft: true,
		ServiceItemIDs: []uuid.UUID{f.corte.ID, f.barba.ID},
	})
	if err != nil || len(draft.Items) != 2 || *draft.Items[0].Price != 25000 {
		t.Fatalf("borrador con servicios: %v %+v", err, draft)
	}
	// Mientras es borrador sube un precio del catálogo: al publicar se congela el de HOY.
	if _, err := f.catalog.Update(f.shop.ID, f.corte.ID, f.owner, ServiceInput{Name: "Corte clásico", Price: intp(31000)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	live, err := ps.Update(draft.ID, f.owner, PublishPromotionInput{EndsAt: inDays(4)}, "active")
	if err != nil || len(live.Items) != 2 || *live.Items[0].Price != 31000 || *live.Items[1].Price != 12000 {
		t.Fatalf("al publicar se vuelve a congelar con el precio de hoy: %v %+v", err, live.Items)
	}
	// Si un servicio se retiró mientras era borrador, publicar lo dice en vez de publicar algo que ya no se presta.
	d2, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Otro combo", Template: models.TemplatePaquete, Draft: true, ServiceItemIDs: []uuid.UUID{f.corte.ID, f.cejas.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.catalog.Retire(f.shop.ID, f.cejas.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Update(d2.ID, f.owner, PublishPromotionInput{EndsAt: inDays(4)}, "active"); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("publicar con un servicio retirado debe rechazarse: %v", err)
	}
}

func TestPromotion_LifecycleLimitAndOwnership(t *testing.T) {
	f := newOfferFixture(t)
	ps := promoService(f)
	mk := func(title string) *models.Promotion {
		p, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: title, Template: models.TemplateCelebrar, EndsAt: inDays(5)})
		if err != nil {
			t.Fatalf("%s: %v", title, err)
		}
		return p
	}
	a, b, c := mk("Uno"), mk("Dos"), mk("Tres")
	if _, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Cuatro", Template: models.TemplateCelebrar, EndsAt: inDays(5)}); err == nil {
		t.Fatal("el límite de 3 activas debe aplicar")
	}
	// Los borradores no cuentan, pero al publicarlos sí chocan con el límite.
	d, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Borrador", Template: models.TemplateCelebrar, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Update(d.ID, f.owner, PublishPromotionInput{EndsAt: inDays(5)}, "active"); err == nil {
		t.Fatal("publicar un borrador con 3 activas debe chocar con el límite")
	}
	// Pausar libera un lugar y la oculta del público; reanudarla la devuelve.
	if _, err := ps.Update(a.ID, f.owner, PublishPromotionInput{}, "paused"); err != nil {
		t.Fatal(err)
	}
	if pub, _ := ps.ActiveForEntity(f.shop.ID); len(pub) != 2 {
		t.Fatalf("pausada no es pública: %d", len(pub))
	}
	if _, err := ps.Update(d.ID, f.owner, PublishPromotionInput{EndsAt: inDays(5)}, "active"); err != nil {
		t.Fatalf("tras pausar una cabe el borrador: %v", err)
	}
	// Finalizar es definitivo.
	if _, err := ps.Update(b.ID, f.owner, PublishPromotionInput{}, "finished"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Update(b.ID, f.owner, PublishPromotionInput{}, "active"); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("una finalizada no se reactiva: %v", err)
	}
	if _, err := ps.Update(b.ID, f.owner, PublishPromotionInput{Title: "Revivida"}, ""); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("una finalizada no se edita: %v", err)
	}
	// Una publicada no vuelve a borrador.
	if _, err := ps.Update(c.ID, f.owner, PublishPromotionInput{}, "draft"); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("publicada no vuelve a borrador: %v", err)
	}
	if _, err := ps.Update(c.ID, f.owner, PublishPromotionInput{}, "inventado"); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("estado inválido: %v", err)
	}

	// Propiedad: otro usuario no edita, publica ni lista; una promoción inexistente es «no encontrada».
	if _, err := ps.Update(c.ID, f.other, PublishPromotionInput{Title: "Robada"}, ""); !errors.Is(err, ErrPromotionNotOwner) {
		t.Fatalf("editar ajena: %v", err)
	}
	if _, err := ps.Publish(f.shop.ID, f.other, PublishPromotionInput{Title: "Intrusa", Template: models.TemplateCelebrar, EndsAt: inDays(2)}); !errors.Is(err, ErrPromotionNotOwner) {
		t.Fatalf("publicar en negocio ajeno: %v", err)
	}
	if _, err := ps.ListByEntity(f.shop.ID, f.other); !errors.Is(err, ErrPromotionNotOwner) {
		t.Fatalf("listar ajeno: %v", err)
	}
	if _, err := ps.Update(uuid.New(), f.owner, PublishPromotionInput{}, ""); !errors.Is(err, ErrPromotionNotFound) {
		t.Fatalf("inexistente: %v", err)
	}

	// Una promoción de antes del editor (sin plantilla) sigue funcionando con precio o descuento.
	f2 := newOfferFixture(t)
	ps2 := promoService(f2)
	if _, err := ps2.Publish(f2.shop.ID, f2.owner, PublishPromotionInput{Title: "De siempre", Discount: "20", EndsAt: inDays(5)}); err != nil {
		t.Fatalf("promoción sin plantilla: %v", err)
	}
	if _, err := ps2.Publish(f2.shop.ID, f2.owner, PublishPromotionInput{Title: "Sin nada", EndsAt: inDays(5)}); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("sin plantilla ni precio: %v", err)
	}
}
