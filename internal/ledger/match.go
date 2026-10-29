package ledger

import (
	"strings"
	"unicode"
)

// Normalize pasa a minúsculas, quita tildes y signos y junta espacios. Con keepDigitPunct deja «.» y «,» (para montos).
func Normalize(s string, keepDigitPunct ...bool) string {
	keep := len(keepDigitPunct) > 0 && keepDigitPunct[0]
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		case 'ñ':
			r = 'n'
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case keep && (r == '.' || r == ','):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var stopWords = map[string]bool{"de": true, "del": true, "la": true, "el": true, "los": true, "las": true, "con": true,
	"y": true, "para": true, "en": true, "a": true, "un": true, "una": true, "unos": true, "unas": true}

// stem quita la «s»/«es» final para que «cortes» case con «corte».
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "es") && strings.ContainsRune("rnldz", rune(w[len(w)-3])):
		return w[:len(w)-2] // panes → pan, flores → flor
	case len(w) > 3 && strings.HasSuffix(w, "s"):
		return w[:len(w)-1] // cortes → corte
	}
	return w
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(Normalize(s)) {
		if stopWords[w] {
			continue
		}
		out[stem(w)] = true
	}
	return out
}

// MatchName busca el nombre más parecido a lo dictado. Casa si todas las palabras de uno están en el otro
// («corte» con «Corte de cabello»). Si dos nombres quedan igual de cerca devuelve ambiguous=true y idx=-1.
func MatchName(query string, names []string) (idx int, ambiguous bool) {
	q := tokens(query)
	if len(q) == 0 {
		return -1, false
	}
	best, bestScore, ties := -1, 0.0, 0
	for i, n := range names {
		c := tokens(n)
		if len(c) == 0 {
			continue
		}
		inter := 0
		for w := range q {
			if c[w] {
				inter++
			}
		}
		if inter != len(q) && inter != len(c) {
			continue
		}
		score := float64(inter) / float64(len(q)+len(c)-inter)
		switch {
		case score > bestScore+1e-9:
			best, bestScore, ties = i, score, 1
		case score > bestScore-1e-9:
			ties++
		}
	}
	if ties > 1 {
		return -1, true
	}
	return best, false
}
