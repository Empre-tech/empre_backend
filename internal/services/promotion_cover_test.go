package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"

	"github.com/google/uuid"
)

func newMedia(t *testing.T, f *offerFixture, contentType string) models.Media {
	t.Helper()
	m := models.Media{S3Key: "uploads/" + uuid.NewString() + ".jpg", OriginalName: "portada.jpg", ContentType: contentType, Size: 120_000}
	if err := f.offers.OfferRepo.DB.Create(&m).Error; err != nil {
		t.Fatalf("crear medio: %v", err)
	}
	return m
}

func TestPromotion_CoverImage(t *testing.T) {
	f := newOfferFixture(t)
	ps := promoService(f)
	jpg := newMedia(t, f, "image/jpeg")
	png := newMedia(t, f, "image/png")
	pdf := newMedia(t, f, "application/pdf")

	// Sin portada: no hay URL (ni clave) en la respuesta.
	plain, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Sin foto", Template: models.TemplateCelebrar, EndsAt: inDays(5)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(dtos.PromotionResponseFrom(plain))
	if strings.Contains(string(raw), "photo_url") || strings.Contains(string(raw), "photo_media_id") {
		t.Fatalf("sin portada no debe haber datos de portada: %s", raw)
	}

	// Con portada válida: la respuesta pública trae la ruta del medio (no una URL prefirmada que caduca).
	withCover, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Con foto", Template: models.TemplateCelebrar, EndsAt: inDays(5), PhotoMediaID: &jpg.ID})
	if err != nil || withCover.PhotoMediaID == nil || *withCover.PhotoMediaID != jpg.ID {
		t.Fatalf("publicar con portada: %v %+v", err, withCover)
	}
	pub, _ := ps.ActiveForEntity(f.shop.ID)
	var found *models.Promotion
	for i := range pub {
		if pub[i].ID == withCover.ID {
			found = &pub[i]
		}
	}
	if found == nil {
		t.Fatalf("la promoción con portada debe ser pública: %+v", pub)
	}
	resp := dtos.PromotionResponseFrom(found)
	if resp.PhotoURL != "/api/images/"+jpg.ID.String() {
		t.Fatalf("la URL de la portada es la ruta pública del medio: %q", resp.PhotoURL)
	}
	public, _ := json.Marshal(resp)
	if !strings.Contains(string(public), `"photo_url":"/api/images/`+jpg.ID.String()+`"`) {
		t.Fatalf("la respuesta pública debe traer photo_url: %s", public)
	}
	if strings.Contains(string(public), "uploads/") || strings.Contains(string(public), "s3") {
		t.Fatalf("no debe filtrarse la clave interna del almacenamiento: %s", public)
	}

	bad := map[string]*uuid.UUID{
		"medio inexistente":        func() *uuid.UUID { id := uuid.New(); return &id }(),
		"archivo que no es imagen": &pdf.ID,
	}
	for name, id := range bad {
		before := countPromos(t, f)
		if _, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Mala portada", Template: models.TemplateCelebrar, EndsAt: inDays(5), PhotoMediaID: id}); !errors.Is(err, ErrPromotionInvalid) {
			t.Errorf("%s al publicar: debía dar ErrPromotionInvalid, dio %v", name, err)
		}
		if countPromos(t, f) != before {
			t.Errorf("%s: no debe quedar una promoción a medias", name)
		}
	}

	// Editar: cambiar la portada; una rechazada deja la anterior; no mandar portada la conserva.
	upd, err := ps.Update(withCover.ID, f.owner, PublishPromotionInput{PhotoMediaID: &png.ID}, "")
	if err != nil || upd.PhotoMediaID == nil || *upd.PhotoMediaID != png.ID {
		t.Fatalf("cambiar la portada: %v %+v", err, upd)
	}
	if _, err := ps.Update(withCover.ID, f.owner, PublishPromotionInput{PhotoMediaID: &pdf.ID}, ""); !errors.Is(err, ErrPromotionInvalid) {
		t.Fatalf("una portada inválida al editar se rechaza: %v", err)
	}
	if _, err := ps.Update(withCover.ID, f.owner, PublishPromotionInput{Title: "Con foto (editada)"}, ""); err != nil {
		t.Fatal(err)
	}
	var stored models.Promotion
	f.offers.OfferRepo.DB.First(&stored, "id = ?", withCover.ID)
	if stored.PhotoMediaID == nil || *stored.PhotoMediaID != png.ID || stored.Title != "Con foto (editada)" {
		t.Fatalf("tras un rechazo y una edición de texto la portada sigue siendo la última válida: %+v", stored)
	}

	// Un borrador también puede llevar portada y la conserva al publicarse.
	draft, err := ps.Publish(f.shop.ID, f.owner, PublishPromotionInput{Title: "Borrador con foto", Template: models.TemplateCelebrar, Draft: true, PhotoMediaID: &jpg.ID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := ps.Update(draft.ID, f.owner, PublishPromotionInput{EndsAt: inDays(4)}, "active")
	if err != nil || live.PhotoMediaID == nil || *live.PhotoMediaID != jpg.ID {
		t.Fatalf("publicar un borrador conserva la portada: %v %+v", err, live)
	}
}

func countPromos(t *testing.T, f *offerFixture) int64 {
	t.Helper()
	var n int64
	f.offers.OfferRepo.DB.Model(&models.Promotion{}).Where("entity_id = ?", f.shop.ID).Count(&n)
	return n
}
