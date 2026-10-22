package dtos

import (
	"time"

	"github.com/google/uuid"
)

// PhotoResponse is a simplified photo/video view. A "publicacion" can be a
// photo or a short video; the frontend tells them apart from ContentType
// (e.g. "video/mp4" vs "image/jpeg") instead of a separate boolean, so this
// stays correct even if we add more media types later.
type PhotoResponse struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	Order       int       `json:"order"`
	Caption     string    `json:"caption"`
	ContentType string    `json:"content_type"`
	// CreatedAt is the owning Post's creation time (when it was published),
	// used to show "Publicado hace X días" in the post viewer.
	CreatedAt time.Time `json:"created_at"`
}

// PostItemResponse es un archivo (foto o video) dentro de una publicación.
type PostItemResponse struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	ContentType string    `json:"content_type"`
}

// PostResponse es una publicación del negocio: su descripción compartida y sus archivos, en orden.
type PostResponse struct {
	ID        uuid.UUID          `json:"id"`
	Caption   string             `json:"caption"`
	CreatedAt time.Time          `json:"created_at"`
	Items     []PostItemResponse `json:"items"`
}
