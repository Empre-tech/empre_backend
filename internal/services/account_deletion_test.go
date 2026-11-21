package services

import (
	"errors"
	"testing"
	"time"

	"empre_backend/internal/models"

	"golang.org/x/crypto/bcrypt"
)

func TestAccountDelete(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.RefreshToken{}, &models.PushToken{}, &models.Favorite{}, &models.PasswordResetToken{},
		&models.Offer{}, &models.OfferItem{}, &models.Promotion{}, &models.PromotionItem{}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("secreta123"), bcrypt.MinCost)
	photo := models.Media{S3Key: "profiles/rosa.jpg"}
	if err := db.Create(&photo).Error; err != nil {
		t.Fatal(err)
	}
	owner := models.User{Name: "Rosa", Email: "rosa@x.co", PasswordHash: string(hash), Phone: "3001234567", ProfileMediaID: &photo.ID}
	other := models.User{Name: "Otro", Email: "otro@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Tiendas"}
	db.Create(&cat)
	mine := models.Entity{OwnerID: owner.ID, Name: "Tienda Rosa", CategoryID: cat.ID}
	theirs := models.Entity{OwnerID: other.ID, Name: "Tienda Otro", CategoryID: cat.ID}
	for _, e := range []*models.Entity{&mine, &theirs} {
		if err := db.Omit("Owner", "Category").Create(e).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&models.RefreshToken{UserID: owner.ID, Token: "r1", ExpiresAt: time.Now().Add(time.Hour)})
	db.Create(&models.RefreshToken{UserID: other.ID, Token: "r2", ExpiresAt: time.Now().Add(time.Hour)})
	db.Create(&models.Favorite{UserID: owner.ID, EntityID: theirs.ID})
	db.Create(&models.PushToken{UserID: owner.ID, Token: "ExponentPushToken[x]"})

	svc := NewAccountService(db)
	if err := svc.Delete(owner.ID, ""); !errors.Is(err, ErrDeletePasswordRequired) {
		t.Fatalf("sin contraseña: %v", err)
	}
	if err := svc.Delete(owner.ID, "mala"); !errors.Is(err, ErrDeleteWrongPassword) {
		t.Fatalf("contraseña mala: %v", err)
	}
	if err := svc.Delete(owner.ID, "secreta123"); err != nil {
		t.Fatal(err)
	}

	// La cuenta ya no se encuentra, y sus datos personales quedaron borrados.
	var n int64
	db.Model(&models.User{}).Where("id = ?", owner.ID).Count(&n)
	if n != 0 {
		t.Fatal("la cuenta sigue visible")
	}
	var gone models.User
	db.Unscoped().First(&gone, "id = ?", owner.ID)
	if gone.Email == "rosa@x.co" || gone.Name != "Usuario eliminado" || gone.Phone != "" || gone.PasswordHash != "" {
		t.Fatalf("datos personales sin borrar: %+v", gone)
	}
	db.Model(&models.Media{}).Where("id = ?", photo.ID).Count(&n)
	if n != 0 {
		t.Fatal("la foto de perfil se sigue sirviendo")
	}
	// El correo queda libre.
	if err := db.Create(&models.User{Name: "Rosa", Email: "rosa@x.co", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("no se puede volver a usar el correo: %v", err)
	}
	// Su negocio no se ve; el del otro sí.
	db.Model(&models.Entity{}).Where("id = ?", mine.ID).Count(&n)
	if n != 0 {
		t.Fatal("el negocio sigue visible")
	}
	db.Model(&models.Entity{}).Where("id = ?", theirs.ID).Count(&n)
	if n != 1 {
		t.Fatal("se borró el negocio de otro")
	}
	// Sesiones, favoritos y push: los suyos fuera, los del otro intactos.
	db.Model(&models.RefreshToken{}).Where("user_id = ?", owner.ID).Count(&n)
	if n != 0 {
		t.Fatal("quedaron sesiones abiertas")
	}
	db.Model(&models.RefreshToken{}).Where("user_id = ?", other.ID).Count(&n)
	if n != 1 {
		t.Fatal("se cerró la sesión de otro")
	}
	for _, m := range []any{&models.Favorite{}, &models.PushToken{}} {
		db.Model(m).Where("user_id = ?", owner.ID).Count(&n)
		if n != 0 {
			t.Fatalf("quedó %T", m)
		}
	}
	if err := svc.Delete(owner.ID, "secreta123"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("segunda vez: %v", err)
	}
}
