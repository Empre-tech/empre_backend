package services

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ---------- cuentas abiertas (fase 8) ----------

// TabsEnabled enciende las cuentas abiertas en el dictado. Por ahora están construidas pero apagadas: la app no las
// muestra y el dictado registra «mesa 3: dos bandejas» como una venta normal.
var TabsEnabled = false

// maxOpenTabs y maxTabLines ponen un techo razonable para un solo celular.
const (
	maxOpenTabs = 200
	maxTabLines = 200
)

// TabView es una cuenta con su total, para la pantalla «Mesas» / «Cuentas».
type TabView struct {
	models.OpenTab
	Total int `json:"total"`
}

func tabTotal(t models.OpenTab) int {
	sum := 0
	for _, l := range t.Lines {
		if !l.Removed {
			sum += int(math.Round(l.Qty * float64(l.UnitPrice)))
		}
	}
	return sum
}

// OpenTabInput abre una cuenta. ID lo genera la app; abrirla otra vez con el mismo id no hace nada.
type OpenTabInput struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	AppointmentID *uuid.UUID `json:"appointment_id"`
}

// OpenTab abre una cuenta (idempotente por id: el id lo pone la app, que evita abrir dos con el mismo nombre). Desde
// una cita, la cuenta arranca con el servicio reservado y el nombre del cliente.
func (s *LedgerService) OpenTab(ownerID, entityID uuid.UUID, in OpenTabInput) (TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return TabView{}, err
	}
	if in.ID == uuid.Nil {
		return TabView{}, &InvalidError{"falta el id de la cuenta"}
	}
	var existing models.OpenTab
	if err := s.db.Where("id = ? AND entity_id = ?", in.ID, entityID).First(&existing).Error; err == nil {
		return s.tabView(entityID, existing.ID)
	}
	name := cleanText(in.Name, 40)
	var appt *models.Appointment
	if in.AppointmentID != nil {
		var a models.Appointment
		if err := s.db.Where("id = ? AND entity_id = ?", *in.AppointmentID, entityID).First(&a).Error; err != nil {
			return TabView{}, &InvalidError{"esa cita no es de este negocio"}
		}
		appt = &a
		if name == "" && a.CustomerID != nil {
			var u models.User
			if err := s.db.Select("id", "name").First(&u, "id = ?", *a.CustomerID).Error; err == nil {
				name = cleanText(u.Name, 40)
			}
		}
	}
	if name == "" {
		return TabView{}, &InvalidError{"ponle un nombre a la cuenta (Mesa 3, Juan…)"}
	}
	var open int64
	s.db.Model(&models.OpenTab{}).Where("entity_id = ? AND status = ?", entityID, models.TabOpen).Count(&open)
	if open >= maxOpenTabs {
		return TabView{}, &InvalidError{"tienes demasiadas cuentas abiertas: cobra o cancela algunas"}
	}
	tab := models.OpenTab{ID: in.ID, EntityID: entityID, Name: name, Status: models.TabOpen, OpenedAt: nowCO(), AppointmentID: in.AppointmentID}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&tab)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		if appt != nil && appt.ServiceID != nil {
			var it models.ServiceItem
			if err := tx.Where("id = ? AND entity_id = ?", *appt.ServiceID, entityID).First(&it).Error; err == nil {
				price := 0
				if it.Price != nil {
					price = *it.Price
				}
				line := models.OpenTabLine{ID: uuid.NewSHA1(in.ID, []byte("appt")), TabID: in.ID, ItemID: &it.ID, Name: it.Name, Qty: 1, UnitPrice: price}
				return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error
			}
		}
		return nil
	})
	if err != nil {
		return TabView{}, err
	}
	return s.tabView(entityID, in.ID)
}

func (s *LedgerService) tabView(entityID, tabID uuid.UUID) (TabView, error) {
	var t models.OpenTab
	err := s.db.Preload("Lines", func(db *gorm.DB) *gorm.DB { return db.Order("created_at") }).
		Where("id = ? AND entity_id = ?", tabID, entityID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TabView{}, ErrLedgerNotFound
	}
	if err != nil {
		return TabView{}, err
	}
	return TabView{OpenTab: t, Total: tabTotal(t)}, nil
}

// ListTabs devuelve las cuentas abiertas, las más antiguas primero.
func (s *LedgerService) ListTabs(ownerID, entityID uuid.UUID) ([]TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var tabs []models.OpenTab
	if err := s.db.Preload("Lines", func(db *gorm.DB) *gorm.DB { return db.Order("created_at") }).
		Where("entity_id = ? AND status = ?", entityID, models.TabOpen).Order("opened_at").Find(&tabs).Error; err != nil {
		return nil, err
	}
	out := make([]TabView, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, TabView{OpenTab: t, Total: tabTotal(t)})
	}
	return out, nil
}

// TabLineInput agrega algo a una cuenta. ID lo genera la app (idempotente). Sin precio, se usa el del catálogo.
type TabLineInput struct {
	ID        uuid.UUID  `json:"id"`
	ItemID    *uuid.UUID `json:"item_id"`
	Name      string     `json:"name"`
	Qty       float64    `json:"qty"`
	UnitPrice *int       `json:"unit_price"`
}

// openTabFor carga una cuenta abierta (con bloqueo) o explica por qué no se puede tocar.
func openTabFor(tx *gorm.DB, entityID, tabID uuid.UUID) (models.OpenTab, error) {
	var t models.OpenTab
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND entity_id = ?", tabID, entityID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return t, ErrLedgerNotFound
	}
	if err != nil {
		return t, err
	}
	if t.Status != models.TabOpen {
		return t, &InvalidError{"la cuenta «" + t.Name + "» ya se cerró"}
	}
	return t, nil
}

// AddToTab agrega ítems a una cuenta abierta (idempotente por id de cada renglón).
func (s *LedgerService) AddToTab(ownerID, entityID, tabID uuid.UUID, lines []TabLineInput) (TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return TabView{}, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		return s.addToTab(tx, entityID, tabID, lines)
	})
	if err != nil {
		return TabView{}, err
	}
	return s.tabView(entityID, tabID)
}

func (s *LedgerService) addToTab(tx *gorm.DB, entityID, tabID uuid.UUID, lines []TabLineInput) error {
	if len(lines) == 0 {
		return &InvalidError{"no hay nada para agregar"}
	}
	t, err := openTabFor(tx, entityID, tabID)
	if err != nil {
		// Un renglón que ya estaba (reenvío) no es un error aunque la cuenta ya se haya cerrado.
		if len(lines) > 0 && lines[0].ID != uuid.Nil {
			var n int64
			tx.Model(&models.OpenTabLine{}).Where("id = ? AND tab_id = ?", lines[0].ID, tabID).Count(&n)
			if n > 0 {
				return nil
			}
		}
		return err
	}
	var count int64
	tx.Model(&models.OpenTabLine{}).Where("tab_id = ?", t.ID).Count(&count)
	if int(count)+len(lines) > maxTabLines {
		return &InvalidError{"la cuenta tiene demasiados renglones"}
	}
	for _, l := range lines {
		if l.ID == uuid.Nil {
			return &InvalidError{"falta el id del renglón"}
		}
		if l.Qty <= 0 || l.Qty > 10000 || math.IsNaN(l.Qty) {
			return &InvalidError{"la cantidad debe ser mayor que cero"}
		}
		row := models.OpenTabLine{ID: l.ID, TabID: t.ID, Qty: ledger.RoundQty(l.Qty), Name: cleanText(l.Name, 80)}
		if l.ItemID != nil {
			var it models.ServiceItem
			if err := tx.Where("id = ? AND entity_id = ? AND status = ?", *l.ItemID, entityID, models.ServiceActive).First(&it).Error; err != nil {
				return &InvalidError{"un ítem de la cuenta no existe en este negocio"}
			}
			if many, err := hasVariants(tx, it.ID); err != nil {
				return err
			} else if many {
				return &InvalidError{"«" + it.Name + "» tiene variantes: elige cuál"}
			}
			price := it.Price
			if price == nil && it.ParentID != nil {
				var p models.ServiceItem
				if err := tx.Select("id", "price").First(&p, "id = ?", *it.ParentID).Error; err == nil {
					price = p.Price
				}
			}
			row.ItemID = &it.ID
			if row.Name == "" {
				row.Name = it.Name
			}
			switch {
			case l.UnitPrice != nil:
				row.UnitPrice = *l.UnitPrice
			case price != nil:
				row.UnitPrice = *price
			default:
				return &InvalidError{"«" + it.Name + "» no tiene precio: escríbelo"}
			}
		} else {
			if row.Name == "" || l.UnitPrice == nil {
				return &InvalidError{"cada renglón necesita nombre y precio"}
			}
			row.UnitPrice = *l.UnitPrice
		}
		if row.UnitPrice < 0 || row.UnitPrice > 10_000_000 {
			return &InvalidError{"precio inválido"}
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return tx.Model(&models.OpenTab{}).Where("id = ?", t.ID).Update("updated_at", time.Now()).Error
}

// RemoveFromTab quita un renglón de una cuenta abierta (queda marcado, no se borra).
func (s *LedgerService) RemoveFromTab(ownerID, entityID, tabID, lineID uuid.UUID) (TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return TabView{}, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := openTabFor(tx, entityID, tabID); err != nil {
			return err
		}
		res := tx.Model(&models.OpenTabLine{}).Where("id = ? AND tab_id = ?", lineID, tabID).Update("removed", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrLedgerNotFound
		}
		return nil
	})
	if err != nil {
		return TabView{}, err
	}
	return s.tabView(entityID, tabID)
}

// PayTabInput cobra una cuenta. SaleID es el client_id de la venta que se crea (idempotente). Credit = fiado: necesita
// el cliente (PartyID), y la deuda queda en sus fiados.
type PayTabInput struct {
	SaleID  uuid.UUID  `json:"sale_id"`
	Method  string     `json:"method"`
	PartyID *uuid.UUID `json:"party_id"`
	Tip     int        `json:"tip"`
	Source  string     `json:"source"`
}

// PayTab convierte la cuenta en una venta normal (ventas, inventario y avisos funcionan igual). Cobrarla otra vez
// devuelve la misma venta.
func (s *LedgerService) PayTab(ownerID, entityID, tabID uuid.UUID, in PayTabInput) (TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return TabView{}, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		return s.payTab(tx, entityID, tabID, in)
	})
	if err != nil {
		return TabView{}, err
	}
	return s.tabView(entityID, tabID)
}

func (s *LedgerService) payTab(tx *gorm.DB, entityID, tabID uuid.UUID, in PayTabInput) error {
	if in.SaleID == uuid.Nil {
		return &InvalidError{"falta el id de la venta"}
	}
	if in.Tip < 0 || in.Tip > 10_000_000 {
		return &InvalidError{"propina inválida"}
	}
	var cur models.OpenTab
	if err := tx.Where("id = ? AND entity_id = ?", tabID, entityID).First(&cur).Error; err == nil && cur.Status == models.TabPaid && cur.SaleID != nil && *cur.SaleID == in.SaleID {
		return nil // reenvío
	}
	t, err := openTabFor(tx, entityID, tabID)
	if err != nil {
		return err
	}
	var lines []models.OpenTabLine
	if err := tx.Where("tab_id = ? AND removed = false", t.ID).Order("created_at").Find(&lines).Error; err != nil {
		return err
	}
	if len(lines) == 0 {
		return &InvalidError{"la cuenta está vacía: agrega algo o cancélala"}
	}
	sale := SaleInput{ClientID: in.SaleID, Method: in.Method, PartyID: in.PartyID, Note: "Cuenta: " + t.Name, Source: in.Source}
	for _, l := range lines {
		price := l.UnitPrice
		sale.Lines = append(sale.Lines, LineInput{ItemID: l.ItemID, Name: l.Name, Qty: l.Qty, UnitPrice: &price})
	}
	created, _, err := s.createSale(entityID, sale, tx)
	if err != nil {
		return err
	}
	now := nowCO()
	return tx.Model(&models.OpenTab{}).Where("id = ?", t.ID).
		Updates(map[string]any{"status": models.TabPaid, "closed_at": now, "sale_id": created.ID, "tip": in.Tip}).Error
}

// CancelTab cierra una cuenta sin cobrarla (no toca plata ni inventario).
func (s *LedgerService) CancelTab(ownerID, entityID, tabID uuid.UUID, reason string) (TabView, error) {
	if err := s.requireOwner(ownerID, entityID); err != nil {
		return TabView{}, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var cur models.OpenTab
		if err := tx.Where("id = ? AND entity_id = ?", tabID, entityID).First(&cur).Error; err == nil && cur.Status == models.TabCancelled {
			return nil
		}
		if _, err := openTabFor(tx, entityID, tabID); err != nil {
			return err
		}
		return tx.Model(&models.OpenTab{}).Where("id = ?", tabID).
			Updates(map[string]any{"status": models.TabCancelled, "closed_at": nowCO(), "cancel_reason": cleanText(reason, 120)}).Error
	})
	if err != nil {
		return TabView{}, err
	}
	return s.tabView(entityID, tabID)
}

// openTabByName busca una cuenta abierta por su nombre (para la voz): exacto primero, luego el que lo contenga.
func (s *LedgerService) openTabByName(db *gorm.DB, entityID uuid.UUID, name string) (*models.OpenTab, error) {
	var tabs []models.OpenTab
	if err := db.Where("entity_id = ? AND status = ?", entityID, models.TabOpen).Find(&tabs).Error; err != nil {
		return nil, err
	}
	key := ledger.Normalize(name)
	sort.SliceStable(tabs, func(i, j int) bool { return len(tabs[i].Name) < len(tabs[j].Name) })
	for i := range tabs {
		if ledger.Normalize(tabs[i].Name) == key {
			return &tabs[i], nil
		}
	}
	for i := range tabs {
		n := ledger.Normalize(tabs[i].Name)
		if key != "" && (strings.Contains(n, key) || strings.Contains(key, n)) {
			return &tabs[i], nil
		}
	}
	return nil, nil
}

// openTabsNote: cuentas que siguen abiertas desde ayer o hace más de 6 horas (para el aviso del día).
func (s *LedgerService) staleTabs(entityID uuid.UUID) (int64, error) {
	var n int64
	now := nowCO()
	y, m, d := now.Date()
	startOfDay := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	err := s.db.Model(&models.OpenTab{}).Where("entity_id = ? AND status = ? AND (opened_at < ? OR opened_at < ?)",
		entityID, models.TabOpen, startOfDay, now.Add(-6*time.Hour)).Count(&n).Error
	return n, err
}
