package ledger

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ParseSpokenAmount lee un monto dicho o escrito como lo dice la gente: «25.000», «25 mil», «25k», «veinte mil»,
// «veinte lucas», «dos quinientos» (2.500), «un millón quinientos mil», «dos palos». Devuelve ok=false si no está
// seguro: es la red de seguridad contra un cero de más, así que ante la duda NO adivina.
func ParseSpokenAmount(text string) (int, bool) {
	toks := strings.Fields(strings.NewReplacer("$", " ", ",", ",", "-", " ").Replace(Normalize(text, true)))
	if len(toks) == 0 {
		return 0, false
	}

	var total, cur float64
	seenAny := false
	seenMil := false
	lastKind, sawY := 0, false // 0 nada o escala · 1 centenas · 2 decenas · 3 unidades · 4 cifra

	flushScale := func(mult float64) {
		if cur == 0 {
			cur = 1
		}
		total += cur * mult
		cur = 0
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t {
		case "pesos", "peso", "cop", "de", "con", "mas":
			continue
		case "y":
			sawY = true
			continue
		case "mil", "luca", "lucas", "k":
			flushScale(1000)
			seenMil, seenAny = true, true
			lastKind, sawY = 0, false
			continue
		case "millon", "millones", "palo", "palos":
			flushScale(1_000_000)
			seenAny = true
			lastKind, sawY = 0, false
			continue
		}
		// «25k», «25mil» pegados.
		if m := suffixRe.FindStringSubmatch(t); m != nil {
			v, ok := parseNumberToken(m[1])
			if !ok {
				return 0, false
			}
			mult := 1000.0
			if strings.HasPrefix(m[2], "m") && m[2] != "mil" {
				mult = 1_000_000
			}
			total += (cur + v) * mult
			cur = 0
			seenAny = true
			lastKind, sawY = 0, false
			continue
		}
		if v, ok := parseNumberToken(t); ok {
			if lastKind != 0 {
				return 0, false
			}
			cur += v
			seenAny = true
			lastKind = 4
			continue
		}
		if w, ok := wordValue(t); ok {
			kind := 3
			switch {
			case w >= 100:
				kind = 1
			case w >= 30 && math.Mod(w, 10) == 0:
				kind = 2
			}
			valid := false
			switch lastKind {
			case 0:
				valid = true
			case 1:
				valid = kind == 2 || kind == 3
			case 2:
				valid = kind == 3 && w <= 9 && sawY
			case 3:
				// «dos quinientos»: una cifra seguida de centenas, sin «mil», son miles.
				if kind == 1 && math.Mod(w, 100) == 0 && w <= 900 && cur > 0 && cur < 100 && total == 0 && !seenMil {
					total += cur * 1000
					cur = 0
					seenMil = true
					valid = true
				}
			}
			if !valid {
				return 0, false
			}
			cur += w
			seenAny = true
			lastKind = kind
			continue
		}
		return 0, false
	}
	if !seenAny {
		return 0, false
	}
	res := total + cur
	if res <= 0 || res > 999_999_999 || math.Abs(res-math.Round(res)) > 0.001 {
		return 0, false
	}
	return int(math.Round(res)), true
}

var suffixRe = regexp.MustCompile(`^(\d+(?:[.,]\d+)*)(k|mil|m)$`)

var (
	groupedRe = regexp.MustCompile(`^\d{1,3}([.,]\d{3})+$`)
	decimalRe = regexp.MustCompile(`^\d+[.,]\d{1,2}$`)
	intRe     = regexp.MustCompile(`^\d+$`)
)

// parseNumberToken lee «25», «25.000», «25,000» (miles) y «1,5» (decimal).
func parseNumberToken(t string) (float64, bool) {
	switch {
	case groupedRe.MatchString(t):
		v, err := strconv.ParseFloat(strings.NewReplacer(".", "", ",", "").Replace(t), 64)
		return v, err == nil
	case decimalRe.MatchString(t):
		v, err := strconv.ParseFloat(strings.Replace(t, ",", ".", 1), 64)
		return v, err == nil
	case intRe.MatchString(t):
		v, err := strconv.ParseFloat(t, 64)
		return v, err == nil
	}
	return 0, false
}

var wordValues = func() map[string]float64 {
	m := map[string]float64{"un": 1, "una": 1, "cien": 100, "ciento": 100}
	for i, w := range unitWords {
		if i > 0 {
			m[Normalize(w, true)] = float64(i)
		}
	}
	for v, w := range tensWords {
		m[w] = float64(v)
	}
	for i, w := range hundredWords {
		if i > 1 {
			m[w] = float64(i * 100)
		}
	}
	m["veintiun"] = 21
	return m
}()

func wordValue(t string) (float64, bool) {
	v, ok := wordValues[t]
	return v, ok
}
