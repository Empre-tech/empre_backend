package main

import (
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"time"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Los ids salen de este espacio y de una clave fija: repetir no duplica y -limpiar sabe exactamente qué quitar.
var ventasNS = uuid.MustParse("6f3f7a52-9c1e-4d3a-8b15-2a7d1c0e4b90")

func vid(parts ...any) uuid.UUID { return uuid.NewSHA1(ventasNS, []byte(fmt.Sprint(parts...))) }

type ropa struct {
	name        string
	price, cost int
}

var ropaItems = []ropa{
	{"Camisa de lino", 65000, 32000}, {"Vestido de playa", 85000, 41000}, {"Sandalias", 45000, 22000}, {"Sombrero", 35000, 15000},
	{"Short de baño", 48000, 21000}, {"Blusa de algodón", 42000, 19000}, {"Jean", 95000, 52000}, {"Camiseta", 28000, 12000},
	{"Tenis", 130000, 78000}, {"Bolso de playa", 55000, 24000},
}

var ropaClientes = []string{"Julián Torres", "Laura Mejía", "Andrés Rincón", "Camilo Martínez", "Valentina Rojas", "Daniela Pertuz"}
var ropaProveedores = []string{"Textiles Magdalena", "Calzado del Caribe"}

const ventasDias = 60

// dayRand: cada día tiene sus propios números. Correr mañana agrega ese día sin cambiar los anteriores.
func dayRand(day string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(day))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// seedVentas llena el negocio con varias semanas de ventas de todos los tipos: efectivo, transferencia, tarjeta, fiado,
// dos formas de pago, pago en parte, anuladas, por canje, por cita y dictadas, además de gastos, abonos, movimientos y
// cierres de caja. Solo agrega; lo que ya tenía el negocio no se toca.
func seedVentas(db *gorm.DB, entityID uuid.UUID) {
	var entity models.Entity
	if err := db.Select("id", "name").First(&entity, "id = ?", entityID).Error; err != nil {
		log.Fatal("No encontré el negocio: ", err)
	}
	if err := db.AutoMigrate(&models.Sale{}, &models.CashMove{}, &models.CashClose{}); err != nil {
		log.Fatal("No se pudieron preparar las tablas: ", err)
	}

	onConflict := clause.OnConflict{DoNothing: true}
	parties := map[string]uuid.UUID{}
	addParty := func(name, kind string) {
		id := vid("party", name)
		parties[name] = id
		p := models.Party{ID: id, EntityID: entityID, ClientID: id, Kind: kind, Name: name, NameKey: ledger.Normalize(name), Phone: fmt.Sprintf("300555%04d", len(parties))}
		if err := db.Clauses(onConflict).Create(&p).Error; err != nil {
			log.Fatal(err)
		}
	}
	for _, n := range ropaClientes {
		addParty(n, models.PartyCustomer)
	}
	for _, n := range ropaProveedores {
		addParty(n, models.PartySupplier)
	}

	now := utils.NowInCartagena()
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	base := 150000 // con cuánto abre la primera caja
	var totalSales, totalExp int
	var sales []models.Sale
	var lines []models.SaleLine
	var exps []models.Expense
	var pays []models.DebtPayment
	var moves []models.CashMove
	var closes []models.CashClose
	owed := map[string]int{}
	owedTo := map[string]int{}

	for d := ventasDias; d >= 0; d-- {
		day := today.AddDate(0, 0, -d)
		key := day.Format("2006-01-02")
		r := dayRand(key)
		wd := day.Weekday()
		// Los domingos casi no se abre, viernes y sábado se vende más, y el negocio crece poco a poco.
		n := 4 + r.Intn(6)
		switch wd {
		case time.Sunday:
			n = r.Intn(3)
		case time.Friday, time.Saturday:
			n += 5 + r.Intn(5)
		}
		n += (ventasDias - d) / 15
		// Una mala semana y un puente bueno, para que «lo normal» tenga algo que decir.
		if d >= 21 && d < 28 {
			n = n / 2
		}
		if d >= 7 && d < 10 {
			n += 8
		}
		at := func() time.Time {
			return day.Add(time.Duration(9*60+r.Intn(10*60)) * time.Minute)
		}
		cash := 0
		for i := 0; i < n; i++ {
			when := at()
			if d == 0 && when.After(now) {
				continue
			}
			id := vid("sale", key, i)
			total, cost := 0, []models.SaleLine{}
			for j, count := 0, 1+r.Intn(3); j < count; j++ {
				it := ropaItems[r.Intn(len(ropaItems))]
				qty := float64(1 + r.Intn(2)*r.Intn(2))
				unitCost := it.cost
				cost = append(cost, models.SaleLine{ID: vid("line", key, i, j), SaleID: id, Name: it.name, Qty: qty, UnitPrice: it.price, UnitCost: &unitCost})
				total += it.price * int(qty)
			}
			sale := models.Sale{ID: id, EntityID: entityID, ClientID: id, OccurredAt: when, Method: models.PayCash, Source: models.SourceManual, Status: models.LedgerActive}
			// De dónde vino.
			switch s := r.Intn(100); {
			case s < 14:
				sale.Source = models.SourceOfferClaim
				total = total * 85 / 100 / 1000 * 1000
				sale.Note = "Canje de Empre Offers"
			case s < 20:
				sale.Source = models.SourceAppointment
				sale.Note = "Cita de asesoría"
			case s < 28:
				sale.Source = models.SourceVoice
			}
			sale.Total = total
			cashPart := 0
			// Cómo se pagó.
			switch p := r.Intn(100); {
			case p < 46:
				cashPart = total
			case p < 70:
				sale.Method = models.PayTransfer
			case p < 80:
				sale.Method = models.PayCard
			case p < 87: // fiado completo
				who := ropaClientes[r.Intn(len(ropaClientes))]
				pid := parties[who]
				sale.Method, sale.PartyID = models.PayCredit, &pid
				owed[who] += total
			case p < 94: // dos formas
				sale.Method2, sale.Amount2 = models.PayTransfer, total/2/1000*1000
				cashPart = total - sale.Amount2
			default: // pagó una parte
				who := ropaClientes[r.Intn(len(ropaClientes))]
				pid := parties[who]
				sale.PartyID, sale.Method2, sale.Amount2 = &pid, models.PayCredit, total/2/1000*1000
				cashPart = total - sale.Amount2
				owed[who] += sale.Amount2
			}
			// Unas pocas anuladas.
			if r.Intn(100) < 3 {
				voided := when.Add(10 * time.Minute)
				sale.Status, sale.VoidedAt, sale.VoidReason = models.LedgerVoided, &voided, "Se equivocó de producto"
				if sale.Method == models.PayCredit && sale.PartyID != nil {
					for _, c := range ropaClientes {
						if parties[c] == *sale.PartyID {
							owed[c] -= total
						}
					}
				} else if sale.Method2 == models.PayCredit && sale.PartyID != nil {
					for _, c := range ropaClientes {
						if parties[c] == *sale.PartyID {
							owed[c] -= sale.Amount2
						}
					}
				}
				cashPart = 0
			} else {
				totalSales += total
			}
			cash += cashPart
			sales = append(sales, sale)
			lines = append(lines, cost...)
		}

		// Gastos.
		addExp := func(tag, cat string, amount int, method string, party string, note string) {
			id := vid("exp", key, tag)
			e := models.Expense{ID: id, EntityID: entityID, ClientID: id, OccurredAt: at(), Amount: amount, Category: cat, Method: method, Source: models.SourceManual, Status: models.LedgerActive, Note: note}
			if d == 0 && e.OccurredAt.After(now) {
				return
			}
			if party != "" {
				pid := parties[party]
				e.PartyID = &pid
			}
			exps = append(exps, e)
			totalExp += amount
			if method == models.PayCash {
				cash -= amount
			}
		}
		// La mercancía es lo que más pesa: dos compras por semana, una de cada tres a crédito.
		if wd == time.Monday || wd == time.Thursday {
			prov := ropaProveedores[r.Intn(len(ropaProveedores))]
			if r.Intn(3) == 0 {
				amount := 1500000 + r.Intn(8)*100000
				addExp("mercancia", "mercancia", amount, models.PayCredit, prov, "Mercancía a crédito")
				owedTo[prov] += amount
			} else {
				addExp("mercancia", "mercancia", 1400000+r.Intn(8)*100000, models.PayTransfer, prov, "Mercancía")
			}
		}
		// A los proveedores se les abona cada tanto.
		for _, prov := range ropaProveedores {
			if owedTo[prov] > 0 && r.Intn(10) == 0 {
				when := at()
				if d == 0 && when.After(now) {
					continue
				}
				amount := owedTo[prov] / 2 / 1000 * 1000
				id := vid("pay", key, prov)
				pays = append(pays, models.DebtPayment{ID: id, EntityID: entityID, ClientID: id, PartyID: parties[prov], Direction: models.DebtPayable, Amount: amount, Method: models.PayTransfer, OccurredAt: when})
				owedTo[prov] -= amount
			}
		}
		if day.Day() == 1 {
			addExp("arriendo", "arriendo", 900000, models.PayTransfer, "", "Arriendo del local")
		}
		if day.Day() == 5 {
			addExp("servicios", "servicios", 180000, models.PayTransfer, "", "Luz e internet")
		}
		if day.Day() == 15 || day.Day() == 30 {
			addExp("nomina", "nomina", 650000, models.PayTransfer, "", "Pago de la quincena")
		}
		if r.Intn(4) == 0 {
			addExp("transporte", "transporte", 8000+r.Intn(5)*2000, models.PayCash, "", "Mototaxi")
		}
		if r.Intn(9) == 0 {
			addExp("publicidad", "publicidad", 40000, models.PayCard, "", "Volantes")
		}

		// Abonos de fiados.
		for _, who := range ropaClientes {
			if owed[who] > 0 && r.Intn(8) == 0 {
				amount := owed[who]
				if r.Intn(2) == 0 {
					amount = amount / 2 / 1000 * 1000
				}
				when := at()
				if amount <= 0 || (d == 0 && when.After(now)) {
					continue
				}
				id := vid("pay", key, who)
				method := models.PayCash
				if r.Intn(2) == 0 {
					method = models.PayTransfer
				} else {
					cash += amount
				}
				pays = append(pays, models.DebtPayment{ID: id, EntityID: entityID, ClientID: id, PartyID: parties[who], Direction: models.DebtReceivable, Amount: amount, Method: method, OccurredAt: when})
				owed[who] -= amount
			}
		}

		// Saqué o metí plata.
		if r.Intn(5) == 0 {
			when := at()
			if !(d == 0 && when.After(now)) {
				id := vid("move", key)
				dir, reason, amount := models.CashOut, []string{"para_mi", "al_banco"}[r.Intn(2)], 20000+r.Intn(4)*20000
				if r.Intn(4) == 0 {
					dir, reason, amount = models.CashIn, "sencillo", 30000
				}
				moves = append(moves, models.CashMove{ID: id, EntityID: entityID, ClientID: id, OccurredAt: when, Direction: dir, Amount: amount, Reason: reason, Status: models.LedgerActive})
				if dir == models.CashIn {
					cash += amount
				} else {
					cash -= amount
				}
			}
		}

		expected := base + cash
		// Cierre: casi todos los días menos hoy; algún día falta plata y algunos domingos no se cierra.
		if d > 0 && !(wd == time.Sunday && r.Intn(2) == 0) {
			counted := expected
			if r.Intn(7) == 0 {
				counted -= (1 + r.Intn(5)) * 2000
			}
			if counted < 0 {
				counted = 0
			}
			next := 150000
			if counted < next {
				next = counted
			}
			closes = append(closes, models.CashClose{ID: vid("close", key), EntityID: entityID, Day: key, Counted: counted, Expected: expected, BaseNext: next})
			base = next
		} else {
			base = expected
		}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Party", "Lines").Clauses(onConflict).CreateInBatches(sales, 200).Error; err != nil {
			return fmt.Errorf("ventas: %w", err)
		}
		if err := tx.Clauses(onConflict).CreateInBatches(lines, 300).Error; err != nil {
			return fmt.Errorf("renglones: %w", err)
		}
		if err := tx.Omit("Party", "Lines").Clauses(onConflict).CreateInBatches(exps, 200).Error; err != nil {
			return fmt.Errorf("gastos: %w", err)
		}
		if len(pays) > 0 {
			if err := tx.Clauses(onConflict).CreateInBatches(pays, 200).Error; err != nil {
				return fmt.Errorf("abonos: %w", err)
			}
		}
		if len(moves) > 0 {
			if err := tx.Clauses(onConflict).CreateInBatches(moves, 200).Error; err != nil {
				return fmt.Errorf("movimientos: %w", err)
			}
		}
		if len(closes) > 0 {
			if err := tx.Clauses(onConflict).CreateInBatches(closes, 200).Error; err != nil {
				return fmt.Errorf("cierres: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d ventas ($%d vigentes), %d gastos ($%d), %d abonos, %d movimientos de caja y %d cierres en %d días.\n",
		entity.Name, len(sales), totalSales, len(exps), totalExp, len(pays), len(moves), len(closes), ventasDias+1)
}

// borrarVentas quita solo lo que puso seedVentas: reconoce sus filas porque los ids salen de ventasNS.
func borrarVentas(db *gorm.DB, entityID uuid.UUID) {
	now := utils.NowInCartagena()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var sale, exp, pay, move, closeIDs []uuid.UUID
	for d := 0; d <= 400; d++ {
		key := today.AddDate(0, 0, -d).Format("2006-01-02")
		for i := 0; i < 60; i++ {
			sale = append(sale, vid("sale", key, i))
		}
		for _, t := range []string{"mercancia", "arriendo", "servicios", "nomina", "transporte", "publicidad"} {
			exp = append(exp, vid("exp", key, t))
		}
		for _, w := range ropaClientes {
			pay = append(pay, vid("pay", key, w))
		}
		for _, w := range ropaProveedores {
			pay = append(pay, vid("pay", key, w))
		}
		move = append(move, vid("move", key))
		closeIDs = append(closeIDs, vid("close", key))
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		del := func(table, col string, ids []uuid.UUID) error {
			for i := 0; i < len(ids); i += 2000 {
				end := i + 2000
				if end > len(ids) {
					end = len(ids)
				}
				if err := tx.Exec("DELETE FROM "+table+" WHERE "+col+" IN ?", ids[i:end]).Error; err != nil {
					return fmt.Errorf("%s: %w", table, err)
				}
			}
			return nil
		}
		for _, s := range []struct {
			t, c string
			ids  []uuid.UUID
		}{{"sale_lines", "sale_id", sale}, {"sales", "id", sale}, {"expenses", "id", exp}, {"debt_payments", "id", pay}, {"cash_moves", "id", move}, {"cash_closes", "id", closeIDs}} {
			if err := del(s.t, s.c, s.ids); err != nil {
				return err
			}
		}
		var names []uuid.UUID
		for _, n := range append(append([]string{}, ropaClientes...), ropaProveedores...) {
			names = append(names, vid("party", n))
		}
		return tx.Unscoped().Where("id IN ? AND entity_id = ?", names, entityID).Delete(&models.Party{}).Error
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Quitado lo que sembró -ventas. Lo demás del negocio quedó igual.")
}
