package ledger

import "testing"

func TestPesosInWords(t *testing.T) {
	cases := map[int]string{
		0:         "cero pesos",
		1:         "un peso",
		21:        "veintiún pesos",
		31:        "treinta y un pesos",
		100:       "cien pesos",
		101:       "ciento un pesos",
		1000:      "mil pesos",
		2500:      "dos mil quinientos pesos",
		21000:     "veintiún mil pesos",
		25000:     "veinticinco mil pesos",
		100000:    "cien mil pesos",
		101000:    "ciento un mil pesos",
		380000:    "trescientos ochenta mil pesos",
		1000000:   "un millón de pesos",
		1500000:   "un millón quinientos mil pesos",
		2000000:   "dos millones de pesos",
		21000000:  "veintiún millones de pesos",
		100000000: "cien millones de pesos",
	}
	for n, want := range cases {
		if got := PesosInWords(n); got != want {
			t.Errorf("PesosInWords(%d) = %q, quería %q", n, got, want)
		}
	}
}

func TestAmountInWords(t *testing.T) {
	if got := AmountInWords(25000); got != "veinticinco mil" {
		t.Fatalf("got %q", got)
	}
	if got := AmountInWords(21); got != "veintiuno" {
		t.Fatalf("got %q", got)
	}
}
