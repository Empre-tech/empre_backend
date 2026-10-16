package handlers

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// multipartBody arma un cuerpo multipart con un archivo en el campo `field`.
func multipartBody(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

// Solo los caminos que se rechazan antes de subir nada (el servicio es nil: llegar al almacenamiento haría fallar la
// prueba): campo ausente, archivo que no es imagen y peso sobre el tope, con y sin Content-Length declarado.
func TestMediaUpload_RejectsBeforeUploading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewMediaHandler(nil)
	r := gin.New()
	r.POST("/upload", h.Upload)

	post := func(body io.Reader, contentType string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", contentType)
		r.ServeHTTP(w, req)
		return w
	}

	// Sin el campo «file».
	buf, ct := multipartBody(t, "otro", "foto.jpg", []byte("x"))
	if w := post(buf, ct); w.Code != http.StatusBadRequest {
		t.Errorf("sin campo file: %d (%s)", w.Code, w.Body.String())
	}

	// Un archivo de texto con nombre de imagen: se mira el contenido, no el nombre.
	buf, ct = multipartBody(t, "file", "foto.jpg", []byte("esto no es una imagen, solo texto"))
	if w := post(buf, ct); w.Code != http.StatusBadRequest {
		t.Errorf("archivo que no es imagen: %d (%s)", w.Code, w.Body.String())
	}

	// Un archivo vacío.
	buf, ct = multipartBody(t, "file", "vacia.png", nil)
	if w := post(buf, ct); w.Code != http.StatusBadRequest {
		t.Errorf("archivo vacío: %d (%s)", w.Code, w.Body.String())
	}

	// Sobre el tope, con Content-Length declarado: se corta antes de leer.
	big := bytes.Repeat([]byte("a"), MaxUploadBytes+1024)
	buf, ct = multipartBody(t, "file", "enorme.jpg", big)
	if w := post(buf, ct); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("sobre el tope con longitud declarada: %d (%s)", w.Code, w.Body.String())
	}

	// Sobre el tope, sin longitud declarada (cuerpo en trozos): lo corta el lector con tope.
	buf, ct = multipartBody(t, "file", "enorme.jpg", big)
	if w := post(io.NopCloser(bytes.NewReader(buf.Bytes())), ct); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("sobre el tope sin longitud declarada: %d (%s)", w.Code, w.Body.String())
	}
}

func TestMediaUpload_LimitIsEightMegabytes(t *testing.T) {
	if MaxUploadBytes != 8<<20 {
		t.Errorf("el tope documentado es 8 MB, es %d", MaxUploadBytes)
	}
}
