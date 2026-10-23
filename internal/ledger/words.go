// Package ledger reúne las reglas puras (sin base de datos) del módulo de administración del negocio:
// leer y escribir montos en español, y casar lo dictado con el catálogo.
package ledger

import "strings"

var unitWords = []string{"cero", "uno", "dos", "tres", "cuatro", "cinco", "seis", "siete", "ocho", "nueve", "diez",
	"once", "doce", "trece", "catorce", "quince", "dieciséis", "diecisiete", "dieciocho", "diecinueve", "veinte",
	"veintiuno", "veintidós", "veintitrés", "veinticuatro", "veinticinco", "veintiséis", "veintisiete",
	"veintiocho", "veintinueve"}

var tensWords = map[int]string{30: "treinta", 40: "cuarenta", 50: "cincuenta", 60: "sesenta", 70: "setenta",
	80: "ochenta", 90: "noventa"}

var hundredWords = []string{"", "ciento", "doscientos", "trescientos", "cuatrocientos", "quinientos",
	"seiscientos", "setecientos", "ochocientos", "novecientos"}

// below100 escribe 1..99. apocope pone «un» y «veintiún» donde después va un sustantivo (mil, millones, pesos).
func below100(n int, apocope bool) string {
	if n < 30 {
		if apocope {
			switch n {
			case 1:
				return "un"
			case 21:
				return "veintiún"
			}
		}
		return unitWords[n]
	}
	t, u := n/10*10, n%10
	if u == 0 {
		return tensWords[t]
	}
	if apocope && u == 1 {
		return tensWords[t] + " y un"
	}
	return tensWords[t] + " y " + unitWords[u]
}

// below1000 escribe 1..999.
func below1000(n int, apocope bool) string {
	if n == 100 {
		return "cien"
	}
	parts := make([]string, 0, 2)
	if h := n / 100; h > 0 {
		parts = append(parts, hundredWords[h])
	}
	if r := n % 100; r > 0 {
		parts = append(parts, below100(r, apocope))
	}
	return strings.Join(parts, " ")
}

// AmountInWords escribe un monto entero en español («25000» → «veinticinco mil»). Vale de 0 a 999.999.999.
func AmountInWords(n int) string {
	return wordsOf(n, false)
}

func wordsOf(n int, lastApocope bool) string {
	if n <= 0 {
		return "cero"
	}
	parts := make([]string, 0, 4)
	if m := n / 1_000_000; m > 0 {
		if m == 1 {
			parts = append(parts, "un millón")
		} else {
			parts = append(parts, below1000(m, true)+" millones")
		}
	}
	if k := n % 1_000_000 / 1000; k > 0 {
		if k == 1 {
			parts = append(parts, "mil")
		} else {
			parts = append(parts, below1000(k, true)+" mil")
		}
	}
	if u := n % 1000; u > 0 {
		parts = append(parts, below1000(u, lastApocope))
	}
	return strings.Join(parts, " ")
}

// PesosInWords es lo que la tarjeta de confirmación lee en voz alta: «veinticinco mil pesos».
func PesosInWords(n int) string {
	if n <= 0 {
		return "cero pesos"
	}
	if n == 1 {
		return "un peso"
	}
	w := wordsOf(n, true)
	if n >= 1_000_000 && n%1_000_000 == 0 {
		return w + " de pesos"
	}
	return w + " pesos"
}
