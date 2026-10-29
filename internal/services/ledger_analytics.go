package services

import (
	"fmt"
	"math"
	"sort"
	"time"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// ---------- análisis del negocio (Empre Finance) ----------
//
// Un solo llamado arma todo lo que muestran «Ventas y gastos» y «Análisis del negocio» para un rango de fechas libre:
// totales contra un periodo de comparación, la serie por día (o semana o mes), cuándo se vende más, la ganancia por
// producto y el inventario en plata. Todo en hora de Colombia.

const (
	maxAnalyticsDays  = 366
	maxAnalyticsItems = 300
	analyticsListMax  = 5
)

// PeriodTotals son las cifras de un rango.
type PeriodTotals struct {
	SalesTotal    int            `json:"sales_total"`
	SalesCount    int            `json:"sales_count"`
	ExpensesTotal int            `json:"expenses_total"`
	Profit        int            `json:"profit"`
	AvgTicket     int            `json:"avg_ticket"`
	ByMethod      MethodTotals   `json:"by_method"`
	ExpensesByCat map[string]int `json:"expenses_by_category"`
	// Margen bruto sobre los renglones con costo; MarginCoverage = % de lo vendido que tenía costo.
	GrossMargin    int `json:"gross_margin"`
	MarginCoverage int `json:"margin_coverage_pct"`
	CostedRevenue  int `json:"costed_revenue"` // lo vendido en renglones con costo (base del margen %)
}

// SeriesPoint es una barra del gráfico: un día, una semana o un mes que empieza en Start.
type SeriesPoint struct {
	Start    string `json:"start"` // AAAA-MM-DD
	Sales    int    `json:"sales"`
	Expenses int    `json:"expenses"`
}

// ProductStat: lo vendido de un producto en el rango. Profit/MarginPct solo cuentan renglones con costo.
type ProductStat struct {
	Ref           string   `json:"ref"` // id del catálogo o, si no lo hay, el nombre normalizado
	Name          string   `json:"name"`
	Qty           float64  `json:"qty"`
	Revenue       int      `json:"revenue"`
	CostedRevenue int      `json:"costed_revenue"`
	Profit        *int     `json:"profit"`
	MarginPct     *float64 `json:"margin_pct"`
}

type InvLine struct {
	Ref   uuid.UUID `json:"ref"`
	Name  string    `json:"name"`
	Text  string    `json:"text"`
	Tone  string    `json:"tone"` // out | low | muted
	Value *int      `json:"value,omitempty"`
}

type SectionValue struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// InventoryMoney: el inventario visto en plata.
type InventoryMoney struct {
	ValueCost  int            `json:"value_cost"`  // lo que costó lo que tienes (contado exacto y con costo)
	ValuePrice int            `json:"value_price"` // lo que vale si lo vendes todo al precio de hoy
	Valued     int            `json:"valued"`      // productos que entran en el cálculo
	Products   int            `json:"products"`    // productos que se podrían contar (sin servicios ni agrupadores)
	BySection  []SectionValue `json:"by_section"`
	SoonCount  int            `json:"soon_count"`
	Soon       []InvLine      `json:"soon"`
	LostTotal  int            `json:"lost_total"` // daño, vencimiento o pérdida dentro del rango
	Lost       []InvLine      `json:"lost"`
	IdleTotal  int            `json:"idle_total"` // plata en productos sin ventas hace más de 30 días
	Idle       []InvLine      `json:"idle"`
}

type Analytics struct {
	From        string `json:"from"` // AAAA-MM-DD, ambos incluidos
	To          string `json:"to"`
	Days        int    `json:"days"`
	Compare     string `json:"compare"` // prev | month | year
	CompareFrom string `json:"compare_from"`
	CompareTo   string `json:"compare_to"`

	Current  PeriodTotals `json:"current"`
	Previous PeriodTotals `json:"previous"`

	Bucket string        `json:"bucket"` // day | week | month
	Series []SeriesPoint `json:"series"`
	// Heat[día][franja]: ventas por día de la semana (0 = lunes) y franja (mañana, mediodía, tarde, noche).
	Heat [7][4]int `json:"heat"`

	Products        []ProductStat `json:"products"`
	UncostedCount   int           `json:"uncosted_count"`   // productos vendidos sin costo
	UncostedRevenue int           `json:"uncosted_revenue"` // lo que vendieron

	Receivable int `json:"receivable"`
	Payable    int `json:"payable"`

	Inventory InventoryMoney `json:"inventory"`
}

const dayFmt = "2006-01-02"

// AnalyticsRange interpreta from/to (AAAA-MM-DD, incluidos) en hora de Colombia y devuelve [from, to).
func AnalyticsRange(fromS, toS string) (time.Time, time.Time, error) {
	loc := nowCO().Location()
	from, err := time.ParseInLocation(dayFmt, fromS, loc)
	if err != nil {
		return time.Time{}, time.Time{}, &InvalidError{"from debe ser AAAA-MM-DD"}
	}
	to, err := time.ParseInLocation(dayFmt, toS, loc)
	if err != nil {
		return time.Time{}, time.Time{}, &InvalidError{"to debe ser AAAA-MM-DD"}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, &InvalidError{"la fecha final va después de la inicial"}
	}
	end := to.AddDate(0, 0, 1)
	if days(from, end) > maxAnalyticsDays {
		return time.Time{}, time.Time{}, &InvalidError{"elige como máximo un año"}
	}
	return from, end, nil
}

func days(from, to time.Time) int {
	// Por fecha de calendario (los cambios de hora no aplican en Colombia, pero así no depende de eso).
	return int(math.Round(to.Sub(from).Hours() / 24))
}

// compareRange: el rango con el que se compara. prev = los mismos días justo antes; month = las mismas fechas del mes
// anterior; year = las mismas fechas del año anterior.
func compareRange(mode string, from, to time.Time) (time.Time, time.Time, string) {
	switch mode {
	case "month":
		return from.AddDate(0, -1, 0), to.AddDate(0, -1, 0), mode
	case "year":
		return from.AddDate(-1, 0, 0), to.AddDate(-1, 0, 0), mode
	}
	n := days(from, to)
	return from.AddDate(0, 0, -n), from, "prev"
}

func (s *LedgerService) Analytics(ownerID, entityID uuid.UUID, fromS, toS, compare string) (Analytics, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return Analytics{}, err
	}
	from, to, err := AnalyticsRange(fromS, toS)
	if err != nil {
		return Analytics{}, err
	}
	cf, ct, mode := compareRange(compare, from, to)
	out := Analytics{
		From: from.Format(dayFmt), To: to.AddDate(0, 0, -1).Format(dayFmt), Days: days(from, to),
		Compare: mode, CompareFrom: cf.Format(dayFmt), CompareTo: ct.AddDate(0, 0, -1).Format(dayFmt),
		Products: []ProductStat{},
	}

	cur, sales, exps, err := s.periodTotals(entityID, from, to)
	if err != nil {
		return out, err
	}
	out.Current = cur
	if out.Previous, _, _, err = s.periodTotals(entityID, cf, ct); err != nil {
		return out, err
	}

	// Serie: por día hasta un mes, por semana hasta 4 meses, por mes si es más.
	out.Bucket = "day"
	if out.Days > 120 {
		out.Bucket = "month"
	} else if out.Days > 31 {
		out.Bucket = "week"
	}
	starts := bucketStarts(out.Bucket, from, to)
	idx := map[string]int{}
	for i, st := range starts {
		key := st.Format(dayFmt)
		idx[key] = i
		out.Series = append(out.Series, SeriesPoint{Start: key})
	}
	loc := from.Location()
	for _, sl := range sales {
		t := sl.OccurredAt.In(loc)
		if i, ok := idx[bucketOf(out.Bucket, t).Format(dayFmt)]; ok {
			out.Series[i].Sales += sl.Total
		}
		wd := (int(t.Weekday()) + 6) % 7
		out.Heat[wd][franja(t.Hour())] += sl.Total
	}
	for _, e := range exps {
		if i, ok := idx[bucketOf(out.Bucket, e.OccurredAt.In(loc)).Format(dayFmt)]; ok {
			out.Series[i].Expenses += e.Amount
		}
	}

	out.Products, out.UncostedCount, out.UncostedRevenue = productStats(sales)
	if out.Receivable, out.Payable, err = s.debtTotals(entityID); err != nil {
		return out, err
	}
	if out.Inventory, err = s.inventoryMoney(entityID, from, to); err != nil {
		return out, err
	}
	return out, nil
}

// franja del día: mañana (antes de las 11), mediodía (11 a 3), tarde (3 a 7) y noche (desde las 7).
func franja(h int) int {
	switch {
	case h < 11:
		return 0
	case h < 15:
		return 1
	case h < 19:
		return 2
	}
	return 3
}

func bucketOf(bucket string, t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	switch bucket {
	case "week":
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	case "month":
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
	}
	return d
}

// bucketStarts: el inicio de cada barra entre from y to (la primera puede empezar antes de from).
func bucketStarts(bucket string, from, to time.Time) []time.Time {
	var out []time.Time
	for t := bucketOf(bucket, from); t.Before(to); {
		out = append(out, t)
		switch bucket {
		case "week":
			t = t.AddDate(0, 0, 7)
		case "month":
			t = t.AddDate(0, 1, 0)
		default:
			t = t.AddDate(0, 0, 1)
		}
	}
	return out
}

func (s *LedgerService) periodTotals(entityID uuid.UUID, from, to time.Time) (PeriodTotals, []models.Sale, []models.Expense, error) {
	t := PeriodTotals{ExpensesByCat: map[string]int{}}
	var sales []models.Sale
	if err := s.db.Preload("Lines").Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, from, to).Find(&sales).Error; err != nil {
		return t, nil, nil, err
	}
	costed, lines := 0, 0
	for _, sl := range sales {
		t.SalesTotal += sl.Total
		t.SalesCount++
		switch sl.Method {
		case models.PayCash:
			t.ByMethod.Cash += sl.Total
		case models.PayTransfer:
			t.ByMethod.Transfer += sl.Total
		case models.PayCard:
			t.ByMethod.Card += sl.Total
		case models.PayCredit:
			t.ByMethod.Credit += sl.Total
		}
		for _, l := range saleLineRevenue(sl) {
			lines += l.rev
			if l.line.UnitCost != nil {
				costed += l.rev
				t.GrossMargin += l.rev - int(math.Round(l.line.Qty*float64(*l.line.UnitCost)))
			}
		}
	}
	t.CostedRevenue = costed
	if lines > 0 {
		t.MarginCoverage = costed * 100 / lines
	}
	if t.SalesCount > 0 {
		t.AvgTicket = int(math.Round(float64(t.SalesTotal) / float64(t.SalesCount)))
	}
	var exps []models.Expense
	if err := s.db.Where("entity_id = ? AND status = ? AND occurred_at >= ? AND occurred_at < ?",
		entityID, models.LedgerActive, from, to).Find(&exps).Error; err != nil {
		return t, nil, nil, err
	}
	for _, e := range exps {
		t.ExpensesTotal += e.Amount
		t.ExpensesByCat[e.Category] += e.Amount
	}
	t.Profit = t.SalesTotal - t.ExpensesTotal
	return t, sales, exps, nil
}

type lineRev struct {
	line models.SaleLine
	rev  int
}

// saleLineRevenue reparte el total real de la venta (con descuentos) entre sus renglones.
func saleLineRevenue(sl models.Sale) []lineRev {
	sum := 0
	for _, l := range sl.Lines {
		sum += int(math.Round(l.Qty * float64(l.UnitPrice)))
	}
	out := make([]lineRev, 0, len(sl.Lines))
	for _, l := range sl.Lines {
		gross := int(math.Round(l.Qty * float64(l.UnitPrice)))
		rev := gross
		if sum > 0 {
			rev = int(math.Round(float64(gross) * float64(sl.Total) / float64(sum)))
		}
		out = append(out, lineRev{line: l, rev: rev})
	}
	return out
}

// productStats suma por producto (por id del catálogo; las ventas sueltas, por nombre) y ordena por lo vendido.
func productStats(sales []models.Sale) ([]ProductStat, int, int) {
	type agg struct {
		ProductStat
		profit int
		name   string
	}
	m := map[string]*agg{}
	for _, sl := range sales {
		for _, l := range saleLineRevenue(sl) {
			key := "n:" + ledger.Normalize(l.line.Name)
			if l.line.ItemID != nil {
				key = l.line.ItemID.String()
			}
			a := m[key]
			if a == nil {
				a = &agg{ProductStat: ProductStat{Ref: key}}
				m[key] = a
			}
			a.Name = l.line.Name // el nombre de la venta más reciente que se recorra
			a.Qty += l.line.Qty
			a.Revenue += l.rev
			if l.line.UnitCost != nil {
				a.CostedRevenue += l.rev
				a.profit += l.rev - int(math.Round(l.line.Qty*float64(*l.line.UnitCost)))
			}
		}
	}
	out := make([]ProductStat, 0, len(m))
	uncosted, uncostedRev := 0, 0
	for _, a := range m {
		p := a.ProductStat
		p.Qty = math.Round(p.Qty*1000) / 1000
		if p.CostedRevenue > 0 {
			pr := a.profit
			mp := math.Round(float64(pr)/float64(p.CostedRevenue)*1000) / 10
			p.Profit, p.MarginPct = &pr, &mp
		} else {
			uncosted++
			uncostedRev += p.Revenue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Revenue != out[j].Revenue {
			return out[i].Revenue > out[j].Revenue
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxAnalyticsItems {
		out = out[:maxAnalyticsItems]
	}
	return out, uncosted, uncostedRev
}

// debtTotals: lo que le deben al negocio y lo que debe, hoy.
func (s *LedgerService) debtTotals(entityID uuid.UUID) (int, int, error) {
	var parties []models.Party
	if err := s.db.Where("entity_id = ?", entityID).Find(&parties).Error; err != nil {
		return 0, 0, err
	}
	bal, err := s.balances(entityID)
	if err != nil {
		return 0, 0, err
	}
	rec, pay := 0, 0
	for _, p := range parties {
		b := bal[p.ID]
		if b <= 0 {
			continue
		}
		if p.Kind == models.PartySupplier {
			pay += b
		} else {
			rec += b
		}
	}
	return rec, pay, nil
}

// inventoryMoney: cuánto vale lo que hay, qué se acaba, qué se perdió en el rango y qué plata está quieta.
func (s *LedgerService) inventoryMoney(entityID uuid.UUID, from, to time.Time) (InventoryMoney, error) {
	out := InventoryMoney{BySection: []SectionValue{}, Soon: []InvLine{}, Lost: []InvLine{}, Idle: []InvLine{}}
	ov, err := s.inventory(entityID)
	if err != nil {
		return out, err
	}
	sections := map[string]int{}
	byStock := map[uuid.UUID]InventoryItem{}
	type soonItem struct {
		line InvLine
		rank float64
	}
	var soon []soonItem
	var idleRefs []InventoryItem
	for _, it := range ov.Items {
		if it.StockID != nil {
			byStock[*it.StockID] = it
		}
		isProduct := it.Kind == models.StockProduct && !it.IsService && it.Variants == 0
		if isProduct {
			out.Products++
		}
		if isProduct && it.Mode == ledger.ModeExact && it.Counted && it.Cost != nil && it.Qty > 0 {
			v := int(math.Round(it.Qty * float64(*it.Cost)))
			out.Valued++
			out.ValueCost += v
			if it.Price != nil {
				out.ValuePrice += int(math.Round(it.Qty * float64(*it.Price)))
			}
			sec := it.Section
			if sec == "" {
				sec = "Otros"
			}
			sections[sec] += v
			if it.Sold28 == 0 {
				idleRefs = append(idleRefs, it)
			}
		}
		if it.Mode != ledger.ModeLight && it.Mode != ledger.ModeExact {
			continue
		}
		switch {
		case it.Status == ledger.StatusOut:
			soon = append(soon, soonItem{InvLine{Ref: it.Ref, Name: it.Name, Text: "Se acabó", Tone: "out"}, -1})
		case it.Status == ledger.StatusLow && it.Mode == ledger.ModeExact && it.DaysLeft != nil:
			d := math.Max(1, math.Floor(*it.DaysLeft))
			soon = append(soon, soonItem{InvLine{Ref: it.Ref, Name: it.Name, Text: fmt.Sprintf("Alcanza ~%s %s", fmtQty(d), plural(d, "día", "días")), Tone: "out"}, d})
		case it.Status == ledger.StatusLow && it.Mode == ledger.ModeExact:
			soon = append(soon, soonItem{InvLine{Ref: it.Ref, Name: it.Name, Text: "Quedan " + fmtQty(it.Qty), Tone: "low"}, 50})
		case it.Status == ledger.StatusLow:
			soon = append(soon, soonItem{InvLine{Ref: it.Ref, Name: it.Name, Text: "Queda poco", Tone: "low"}, 60})
		}
	}
	for name, v := range sections {
		out.BySection = append(out.BySection, SectionValue{Name: name, Value: v})
	}
	sort.Slice(out.BySection, func(i, j int) bool { return out.BySection[i].Value > out.BySection[j].Value })
	sort.SliceStable(soon, func(i, j int) bool { return soon[i].rank < soon[j].rank })
	out.SoonCount = len(soon)
	for i := 0; i < len(soon) && i < analyticsListMax; i++ {
		out.Soon = append(out.Soon, soon[i].line)
	}

	// Pérdidas del rango, por insumo o producto y motivo, valoradas al costo de hoy.
	var rows []struct {
		StockItemID uuid.UUID
		Reason      string
		Qty         float64
	}
	if err := s.db.Table("stock_movements").Select("stock_item_id, reason, COALESCE(SUM(-delta),0) AS qty").
		Where("entity_id = ? AND kind = ? AND reason IN ? AND delta < 0 AND occurred_at >= ? AND occurred_at < ?", entityID, models.MoveAdjust,
			[]string{models.ReasonDamaged, models.ReasonExpired, models.ReasonLost}, from, to).
		Group("stock_item_id, reason").Scan(&rows).Error; err != nil {
		return out, err
	}
	why := map[string]string{models.ReasonDamaged: "Dañado", models.ReasonExpired: "Vencido", models.ReasonLost: "Perdido"}
	for _, r := range rows {
		it, ok := byStock[r.StockItemID]
		if !ok {
			continue
		}
		line := InvLine{Ref: it.Ref, Name: it.Name, Text: why[r.Reason] + " · " + fmtQty(r.Qty), Tone: "muted"}
		if it.Cost != nil {
			v := int(math.Round(r.Qty * float64(*it.Cost)))
			line.Value = &v
			out.LostTotal += v
		}
		out.Lost = append(out.Lost, line)
	}
	sort.SliceStable(out.Lost, func(i, j int) bool { return val(out.Lost[i].Value) > val(out.Lost[j].Value) })
	if len(out.Lost) > analyticsListMax {
		out.Lost = out.Lost[:analyticsListMax]
	}

	// Plata quieta: contados, con costo y sin ventas en 4 semanas. Dice hace cuánto fue la última venta.
	if len(idleRefs) > 0 {
		ids := make([]uuid.UUID, 0, len(idleRefs))
		for _, it := range idleRefs {
			ids = append(ids, it.Ref)
		}
		var last []struct {
			ItemID uuid.UUID
			At     time.Time
		}
		if err := s.db.Table("sale_lines").Select("sale_lines.item_id AS item_id, MAX(sales.occurred_at) AS at").
			Joins("JOIN sales ON sales.id = sale_lines.sale_id").
			Where("sales.entity_id = ? AND sales.status = ? AND sale_lines.item_id IN ?", entityID, models.LedgerActive, ids).
			Group("sale_lines.item_id").Scan(&last).Error; err != nil {
			return out, err
		}
		lastAt := map[uuid.UUID]time.Time{}
		for _, l := range last {
			lastAt[l.ItemID] = l.At
		}
		now := nowCO()
		for _, it := range idleRefs {
			v := int(math.Round(it.Qty * float64(*it.Cost)))
			out.IdleTotal += v
			text := "Sin ventas aún"
			if at, ok := lastAt[it.Ref]; ok {
				d := math.Floor(now.Sub(at).Hours() / 24)
				text = fmt.Sprintf("%s días sin vender", fmtQty(d))
			}
			out.Idle = append(out.Idle, InvLine{Ref: it.Ref, Name: it.Name, Text: text, Tone: "muted", Value: &v})
		}
		sort.SliceStable(out.Idle, func(i, j int) bool { return val(out.Idle[i].Value) > val(out.Idle[j].Value) })
		if len(out.Idle) > analyticsListMax {
			out.Idle = out.Idle[:analyticsListMax]
		}
	}
	return out, nil
}

func val(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func plural(n float64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
