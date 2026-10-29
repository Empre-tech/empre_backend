package ledger

import (
	"math"
	"strings"
)

// Reglas puras del inventario fácil: qué nivel de control recomendar según el tipo de negocio y el producto, cuántos
// días alcanza lo que queda y en qué estado está cada producto. Sin base de datos: se prueban con `go test`.

// Modos de control. «none» = no se lleva inventario de ese producto; «light» = semáforo (hay / queda poco / se
// acabó) sin contar; «exact» = cantidad exacta.
const (
	ModeNone  = "none"
	ModeLight = "light"
	ModeExact = "exact"
)

func ValidMode(m string) bool { return m == ModeNone || m == ModeLight || m == ModeExact }

// Niveles del semáforo.
const (
	LevelOK  = "ok"
	LevelLow = "low"
	LevelOut = "out"
)

func ValidLevel(l string) bool { return l == LevelOK || l == LevelLow || l == LevelOut }

// Estado de un producto, para avisos y para el semáforo de la pantalla. «unknown» = se lleva control pero aún no se
// ha contado: no se avisa nada hasta saber cuántos hay.
const (
	StatusOK      = "ok"
	StatusLow     = "low"
	StatusOut     = "out"
	StatusUnknown = "unknown"
	StatusNone    = "none"
)

// Tipos de negocio a efectos de inventario.
const (
	KindShop     = "shop"     // muchas referencias que rotan: tiendas, panaderías, ferreterías, droguerías
	KindValuable = "valuable" // pocas unidades de cada una y de valor: perfumerías, celulares, joyas, ropa, licores
	KindFood     = "food"     // restaurantes: los platos se preparan al pedir
	KindService  = "service"  // peluquerías, spas, talleres: sobre todo servicios, con algunos productos
	KindOther    = "other"
)

// Profile es lo que se sabe del negocio por su categoría.
type Profile struct {
	Kind string
	// Mode es el control recomendado por defecto para sus productos.
	Mode string
	// QuickSale: la «atención rápida» (cuadrícula de platos y un toque para cobrar) es lo principal al vender.
	QuickSale bool
}

// Defaults por tipo de negocio. El tipo lo guarda el negocio (business_profile) y su categoría lo sugiere (profile):
// ya no se adivina por el nombre, salvo como respaldo mientras un dato esté vacío.
var profileByKind = map[string]Profile{
	KindFood:     {KindFood, ModeNone, true},
	KindService:  {KindService, ModeExact, false},
	KindValuable: {KindValuable, ModeExact, false},
	KindShop:     {KindShop, ModeLight, false},
	KindOther:    {KindOther, ModeLight, false},
}

// ValidKind dice si k es uno de los cinco tipos de negocio.
func ValidKind(k string) bool {
	_, ok := profileByKind[k]
	return ok
}

// ProfileForKind devuelve los valores por defecto de un tipo de negocio; uno desconocido se trata como «otro».
func ProfileForKind(kind string) Profile {
	if p, ok := profileByKind[kind]; ok {
		return p
	}
	return profileByKind[KindOther]
}

var categoryKeywords = []struct {
	words []string
	kind  string
}{
	{[]string{"restaurante", "comida", "pizzer", "hamburguesa", "asadero", "pollo", "parrilla", "cafeteria", "heladeria", "sushi", "empanada", "almuerzo", "fritanga", "cocina"}, KindFood},
	{[]string{"peluquer", "barber", "spa", "salon", "belleza", "estetica", "unas", "manicur", "masaje", "taller", "lavander", "tatuaje", "clinica", "odontolog", "veterinar", "gimnasio"}, KindService},
	{[]string{"perfum", "celular", "tecnolog", "joyer", "licor", "ropa", "calzado", "zapat", "boutique", "cosmetic", "juguet", "electrodomest", "relojer", "optica", "accesorios", "bolso", "maquillaje", "tenis", "moda"}, KindValuable},
	{[]string{"tienda", "minimercado", "supermercado", "abarrote", "panader", "fruter", "verdura", "carnicer", "ferreter", "drogueri", "papeler", "miscelanea", "mercado", "graneria", "lacteo", "bodega", "pescader", "farmacia", "dulceri", "cigarrer"}, KindShop},
}

// KindForName adivina el tipo de negocio por el nombre de una categoría. Solo se usa para llenar datos que faltan
// (la migración y el respaldo): devuelve "" si no reconoce el nombre.
func KindForName(name string) string {
	c := Normalize(name)
	for _, g := range categoryKeywords {
		for _, w := range g.words {
			if strings.Contains(c, w) {
				return g.kind
			}
		}
	}
	return ""
}

// ProfileForCategory identifica el tipo de negocio por el nombre de su categoría. Queda como respaldo: lo normal es
// leer el tipo guardado en el negocio (ProfileForKind).
func ProfileForCategory(category string) Profile {
	return ProfileForKind(KindForName(category))
}

// ValuablePrice: desde este precio un producto se cuenta exacto, sea cual sea el negocio: perder uno duele.
const ValuablePrice = 50_000

// FewUnits: hasta esta cantidad de unidades por producto, contar exacto es fácil; más, y el semáforo es mejor.
const FewUnits = 15

// ItemFacts es lo que se sabe de un producto para recomendarle un modo.
type ItemFacts struct {
	Price *int
	// IsService: se agenda o tiene duración (un corte, un tinte): no lleva existencias.
	IsService bool
	// Units: cuántas unidades tiene hoy, si ya se sabe (carga inicial o compras).
	Units *float64
}

// RecommendMode propone el control de un producto. El orden importa: un servicio nunca lleva inventario; lo caro y
// lo que se tiene en pocas unidades se cuenta exacto aunque el negocio tenga cientos de productos; lo que hay a
// montones va en semáforo; si no se sabe nada, manda el tipo de negocio.
func RecommendMode(p Profile, f ItemFacts) string {
	if f.IsService {
		return ModeNone
	}
	if f.Price != nil && *f.Price >= ValuablePrice {
		return ModeExact
	}
	if p.Kind == KindFood {
		return ModeNone // los platos se preparan al pedir: se cuentan las ventas, no las existencias
	}
	if f.Units != nil {
		if *f.Units > FewUnits {
			return ModeLight
		}
		return ModeExact
	}
	return p.Mode
}

// DaysLeft estima cuántos días alcanza lo que queda según lo vendido en las últimas 4 semanas. ok=false si no hay
// ventas con qué calcular.
func DaysLeft(qty, soldLast28 float64) (float64, bool) {
	if soldLast28 <= 0 || qty < 0 {
		return 0, false
	}
	perDay := soldLast28 / 28
	return qty / perDay, true
}

// Status resume el estado de un producto controlado.
//   - light: el nivel que marcó el dueño.
//   - exact: sin contar aún → unknown; 0 o menos → out; bajo el umbral (o para ≤3 días) → low.
func Status(mode string, counted bool, qty float64, level string, lowAt *float64, soldLast28 float64) string {
	switch mode {
	case ModeLight:
		switch level {
		case LevelOut:
			return StatusOut
		case LevelLow:
			return StatusLow
		}
		return StatusOK
	case ModeExact:
		if !counted {
			return StatusUnknown
		}
		if qty <= 0 {
			return StatusOut
		}
		if lowAt != nil && qty <= *lowAt {
			return StatusLow
		}
		if d, ok := DaysLeft(qty, soldLast28); ok && d <= 3 {
			return StatusLow
		}
		return StatusOK
	}
	return StatusNone
}

// SoldOut dice si el producto debe verse «agotado» en el perfil público.
func SoldOut(mode string, counted bool, qty float64, level string) bool {
	switch mode {
	case ModeLight:
		return level == LevelOut
	case ModeExact:
		return counted && qty <= 0
	}
	return false
}

// RoundQty deja las cantidades en milésimas (libras, litros) sin ruido de coma flotante.
func RoundQty(q float64) float64 { return math.Round(q*1000) / 1000 }
