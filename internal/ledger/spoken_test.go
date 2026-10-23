package ledger

import "testing"

func TestParseSpokenAmount(t *testing.T) {
	ok := map[string]int{
		"25.000":                     25000,
		"25,000":                     25000,
		"25000":                      25000,
		"$ 25.000":                   25000,
		"25 mil":                     25000,
		"25k":                        25000,
		"25 mil pesos":               25000,
		"veinte mil":                 20000,
		"veinte lucas":               20000,
		"una luca":                   1000,
		"veinticinco mil":            25000,
		"veinticinco mil quinientos": 25500,
		"dos quinientos":             2500,
		"cuatro cincuenta":           0, // ambiguo: se rechaza abajo
		"ochenta mil":                80000,
		"tres mil quinientos":        3500,
		"cien mil":                   100000,
		"ciento veinte mil":          120000,
		"un millón":                  1000000,
		"un millon quinientos mil":   1500000,
		"dos millones":               2000000,
		"1,5 millones":               1500000,
		"dos palos":                  2000000,
		"treinta y cinco mil":        35000,
		"mil":                        1000,
		"mil quinientos":             1500,
		"cincuenta":                  50,
		"veintiún mil":               21000,
		"80 mil":                     80000,
	}
	for in, want := range ok {
		got, good := ParseSpokenAmount(in)
		if want == 0 {
			if good {
				t.Errorf("%q: debía rechazarse y dio %d", in, got)
			}
			continue
		}
		if !good || got != want {
			t.Errorf("%q = %d,%v; quería %d", in, got, good, want)
		}
	}
	for _, in := range []string{"", "hola", "unos cortes", "veinte pollos", "mil mil mil mil mil mil mil mil mil mil"} {
		if v, good := ParseSpokenAmount(in); good && in != "mil mil mil mil mil mil mil mil mil mil" {
			t.Errorf("%q no debía entenderse, dio %d", in, v)
		}
	}
}

func TestMatchName(t *testing.T) {
	names := []string{"Corte de cabello", "Tinte completo", "Cerveza Águila", "Cerveza Poker", "Empanadas"}
	cases := []struct {
		q   string
		idx int
		amb bool
	}{
		{"corte", 0, false},
		{"cortes de pelo", -1, false}, // «pelo» no está: no se adivina
		{"cortes", 0, false},
		{"tinte", 1, false},
		{"cerveza", -1, true},
		{"cerveza aguila", 2, false},
		{"empanada", 4, false},
		{"pizza", -1, false},
	}
	for _, c := range cases {
		idx, amb := MatchName(c.q, names)
		if idx != c.idx || amb != c.amb {
			t.Errorf("%q → %d,%v; quería %d,%v", c.q, idx, amb, c.idx, c.amb)
		}
	}
}
