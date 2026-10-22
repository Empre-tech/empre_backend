package seedphotos

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"strconv"
	"strings"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Uploader es lo único que el seed necesita del almacenamiento (services.StorageService lo cumple).
type Uploader interface {
	UploadFile(filename string, body io.Reader, contentType string) error
}

// AddToPost sube la foto bajo `prefix` (p. ej. «seed/qr/») y la deja como foto número `order` de la publicación:
// crea su medio y su fila en entity_photos. Devuelve el id del medio (sirve para la foto de perfil).
func AddToPost(tx *gorm.DB, store Uploader, prefix string, post *models.Post, order int, data []byte, ct string) (uuid.UUID, error) {
	key := fmt.Sprintf("%s%s%s", prefix, uuid.NewString(), ExtOf(ct))
	if err := store.UploadFile(key, bytes.NewReader(data), ct); err != nil {
		return uuid.Nil, fmt.Errorf("subir foto: %w", err)
	}
	media := models.Media{S3Key: key, OriginalName: "ejemplo" + ExtOf(ct), ContentType: ct, Size: int64(len(data))}
	if err := tx.Create(&media).Error; err != nil {
		return uuid.Nil, fmt.Errorf("medio: %w", err)
	}
	if err := tx.Omit("Media", "Post").Create(&models.EntityPhoto{PostID: post.ID, EntityID: post.EntityID, MediaID: media.ID, Order: order}).Error; err != nil {
		return uuid.Nil, fmt.Errorf("foto: %w", err)
	}
	return media.ID, nil
}

// ExtOf da la extensión del archivo según su tipo.
func ExtOf(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// Gradient saca los dos colores del degradado de un color de categoría («#F58220»): uno más claro arriba y uno más
// oscuro abajo. Si el color no se entiende, usa un gris azulado.
func Gradient(hex string) (top, bottom color.RGBA) {
	base := color.RGBA{102, 118, 141, 255}
	if h := strings.TrimPrefix(hex, "#"); len(h) >= 6 {
		if v, err := strconv.ParseUint(h[:6], 16, 32); err == nil {
			base = color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
		}
	}
	return mix(base, color.RGBA{255, 255, 255, 255}, 0.25), mix(base, color.RGBA{0, 0, 0, 255}, 0.6)
}
