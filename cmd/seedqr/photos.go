package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
)

// motif es el dibujo sencillo que identifica la categoría en las fotos de ejemplo.
type motif string

const (
	motifPlate motif = "plate" // plato con comida (restaurante)
	motifCup   motif = "cup"   // taza de café
	motifPole  motif = "pole"  // poste de barbería
	motifPaw   motif = "paw"   // huella (mascotas)
	motifPhone motif = "phone" // celular (tecnología)
)

const photoW, photoH = 960, 720

// samplePhoto dibuja una foto de ejemplo (JPEG 960×720): fondo en degradado con los colores dados, luces suaves
// desenfocadas, una mesa abajo y el motivo de la categoría. `variant` cambia la composición para que las fotos de un
// mismo negocio no se vean iguales. No pretende ser una foto real: es para ver cómo lucen las tarjetas con imágenes.
func samplePhoto(top, bottom color.RGBA, m motif, variant int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, photoW, photoH))
	r := rand.New(rand.NewSource(int64(variant)*7919 + int64(len(m))*104729))

	// Degradado diagonal.
	for y := 0; y < photoH; y++ {
		for x := 0; x < photoW; x++ {
			t := (float64(x)/photoW*0.35 + float64(y)/photoH*0.65)
			img.SetRGBA(x, y, mix(top, bottom, t))
		}
	}
	// Luces desenfocadas (bokeh).
	for i := 0; i < 9; i++ {
		cx, cy := r.Float64()*photoW, r.Float64()*photoH*0.6
		rad := 40 + r.Float64()*90
		blob(img, cx, cy, rad, color.RGBA{255, 255, 255, 255}, 0.10+r.Float64()*0.12)
	}
	// Mesa: banda inferior más oscura.
	tableY := photoH * (0.62 + 0.06*float64(variant%3))
	for y := int(tableY); y < photoH; y++ {
		for x := 0; x < photoW; x++ {
			img.SetRGBA(x, y, mix(img.RGBAAt(x, y), color.RGBA{30, 20, 14, 255}, 0.45))
		}
	}

	cx := photoW * (0.38 + 0.24*float64(variant%3)/2)
	cy := tableY - 10
	switch m {
	case motifPlate:
		disc(img, cx, cy, 190, color.RGBA{248, 246, 240, 255})
		disc(img, cx, cy, 150, color.RGBA{236, 232, 222, 255})
		// Comida: montoncitos de colores.
		colors := []color.RGBA{{214, 118, 38, 255}, {120, 168, 64, 255}, {196, 64, 48, 255}, {240, 196, 80, 255}}
		for i := 0; i < 7; i++ {
			a := float64(i) / 7 * 2 * math.Pi
			disc(img, cx+math.Cos(a)*70, cy+math.Sin(a)*50, 42+r.Float64()*14, colors[(i+variant)%len(colors)])
		}
		disc(img, cx, cy, 52, colors[(variant+1)%len(colors)])
	case motifCup:
		disc(img, cx, cy+40, 170, color.RGBA{240, 236, 228, 255}) // plato
		rect(img, cx-95, cy-140, cx+95, cy+40, color.RGBA{250, 248, 244, 255})
		ring(img, cx+120, cy-50, 55, 22, color.RGBA{250, 248, 244, 255})
		disc(img, cx, cy-140, 95, color.RGBA{92, 54, 28, 255}) // café visto desde arriba
		disc(img, cx-20, cy-150, 30, color.RGBA{196, 150, 104, 255})
	case motifPole:
		w := 110.0
		for y := cy - 360; y < cy; y++ {
			for x := cx - w/2; x < cx+w/2; x++ {
				stripe := int((y+(x-cx)*0.8)/36) % 3
				c := []color.RGBA{{230, 40, 50, 255}, {250, 250, 250, 255}, {36, 90, 190, 255}}[(stripe+3)%3]
				set(img, x, y, c)
			}
		}
		rect(img, cx-70, cy-390, cx+70, cy-360, color.RGBA{210, 210, 215, 255})
		rect(img, cx-70, cy, cx+70, cy+30, color.RGBA{210, 210, 215, 255})
	case motifPaw:
		pad := color.RGBA{250, 246, 238, 255}
		disc(img, cx, cy-40, 110, pad)
		for i, dx := range []float64{-120, -45, 45, 120} {
			dy := -170.0
			if i == 0 || i == 3 {
				dy = -120
			}
			disc(img, cx+dx, cy+dy-40, 46, pad)
		}
	case motifPhone:
		rect(img, cx-110, cy-330, cx+110, cy+20, color.RGBA{24, 28, 36, 255})
		rect(img, cx-95, cy-305, cx+95, cy-10, color.RGBA{70, 140, 230, 255})
		disc(img, cx, cy-160, 50, color.RGBA{255, 255, 255, 200})
	}

	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82})
	return buf.Bytes()
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

func set(img *image.RGBA, x, y float64, c color.RGBA) {
	if x >= 0 && y >= 0 && x < photoW && y < photoH {
		img.SetRGBA(int(x), int(y), c)
	}
}

// disc dibuja un círculo lleno con el borde suavizado.
func disc(img *image.RGBA, cx, cy, rad float64, c color.RGBA) {
	for y := cy - rad - 1; y <= cy+rad+1; y++ {
		for x := cx - rad - 1; x <= cx+rad+1; x++ {
			d := math.Hypot(x-cx, y-cy)
			if a := rad + 0.5 - d; a > 0 && x >= 0 && y >= 0 && x < photoW && y < photoH {
				img.SetRGBA(int(x), int(y), mix(img.RGBAAt(int(x), int(y)), c, math.Min(1, a)))
			}
		}
	}
}

// ring dibuja un aro (el asa de la taza).
func ring(img *image.RGBA, cx, cy, rad, width float64, c color.RGBA) {
	for y := cy - rad - 1; y <= cy+rad+1; y++ {
		for x := cx - rad - 1; x <= cx+rad+1; x++ {
			if d := math.Hypot(x-cx, y-cy); d <= rad && d >= rad-width {
				set(img, x, y, c)
			}
		}
	}
}

func rect(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			set(img, x, y, c)
		}
	}
}

// blob es una luz desenfocada: un círculo que se desvanece hacia el borde.
func blob(img *image.RGBA, cx, cy, rad float64, c color.RGBA, strength float64) {
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			d := math.Hypot(x-cx, y-cy) / rad
			if d < 1 && x >= 0 && y >= 0 && x < photoW && y < photoH {
				img.SetRGBA(int(x), int(y), mix(img.RGBAAt(int(x), int(y)), c, strength*(1-d*d)))
			}
		}
	}
}
