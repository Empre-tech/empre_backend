package services

// Los 5 grupos de Explorar (comer, salud, compras, servicios, planes): son solo de presentación y la app los define en
// empre_app/src/lib/categoryGroups.ts. El servidor los necesita para que el mosaico, la pasarela y las rutas de Ofertas
// (3.48) muestren tipos de negocio distintos *como los ve la persona*: «Restaurantes» y «Cafeterías y postres» son dos
// categorías, pero los dos son «Comer y beber». Si cambian en la app, se cambian aquí también (lo vigila
// TestExploreGroupOf). Una categoría que no está en ningún grupo cae en «servicios», igual que en la app.

import "sync"

const exploreFallback = "servicios"

var exploreGroups = map[string]string{
	"restaurantes":          "comer",
	"cafeterias y postres":  "comer",
	"bares y vida nocturna": "comer",
	"belleza y peluqueria":  "salud",
	"salud y bienestar":     "salud",
	"deporte y recreacion":  "salud",
	"tiendas y comercio":    "compras",
	"tecnologia":            "compras",
	"mascotas":              "compras",
	"servicios del hogar":   "servicios",
	"automotriz":            "servicios",
	"educacion":             "servicios",
	"fotografia y diseno":   "servicios",
	"otros":                 "servicios",
	"turismo y hospedaje":   "planes",
	"eventos y fiestas":     "planes",
}

// groupCache recuerda el grupo de cada nombre de categoría (3.53): las rutas y el mosaico de Ofertas lo piden miles
// de veces por petición, dentro de bucles que comparan paradas de a pares, y FoldText es caro. Los nombres de
// categoría son pocos y fijos, así que la caché no crece sin límite.
var groupCache sync.Map

// ExploreGroupOf: el grupo de Explorar de una categoría, por su nombre (sin tildes ni mayúsculas).
func ExploreGroupOf(categoryName string) string {
	if g, ok := groupCache.Load(categoryName); ok {
		return g.(string)
	}
	g, ok := exploreGroups[FoldText(categoryName)]
	if !ok {
		g = exploreFallback
	}
	groupCache.Store(categoryName, g)
	return g
}

// kindOf es el «tipo de negocio» de una oferta para la variedad de Ofertas del barrio: su grupo de Explorar. Sin
// nombre de categoría (datos de prueba), se usa el id de la categoría para no juntar tipos que no se conocen.
func kindOf(c MarketCandidate) string {
	if n := c.Offer.Entity.Category.Name; n != "" {
		return ExploreGroupOf(n)
	}
	return c.Offer.Entity.CategoryID.String()
}
