package seedphotos

import (
	"bytes"
	"image/color"
	"image/jpeg"
	"testing"
)

// Sin motivo también sale un JPEG válido del tamaño de siempre.
func TestSample_NoMotif(t *testing.T) {
	top, bottom := Gradient("#1E5CA8")
	img, err := jpeg.Decode(bytes.NewReader(Sample(top, bottom, MotifNone, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != W || r.Dy() != H {
		t.Fatalf("tamaño %v", r)
	}
}

// El degradado sale del color de la categoría: más claro arriba, más oscuro abajo; un color raro no rompe nada.
func TestGradient(t *testing.T) {
	top, bottom := Gradient("#F58220")
	if !(top.R > 0xF5 && bottom.R < 0xF5 && top.G > bottom.G) {
		t.Fatalf("degradado de #F58220: %v → %v", top, bottom)
	}
	if top, _ := Gradient("rojo"); top == (color.RGBA{}) {
		t.Fatal("sin color de respaldo")
	}
}
