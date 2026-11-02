package database

import (
	"log"

	"empre_backend/internal/models"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

// sectionTemplate: las secciones más comunes de una subcategoría, en orden, de lo que vende (Sell) y de insumos
// (Supply). «Otros» no va aquí: todo negocio lo tiene en los dos tipos, al final.
type sectionTemplate struct {
	Sell   []string
	Supply []string
}

// Nombres de secciones de insumo que se repiten en muchas plantillas.
const (
	supCarnes      = "Carnes y pollo"
	supPescados    = "Pescados y mariscos"
	supVerduras    = "Verduras y frutas"
	supGranos      = "Granos y abarrotes"
	supLacteos     = "Lácteos y huevos"
	supPanes       = "Panes y masas"
	supHarinas     = "Harinas"
	supBebidas     = "Bebidas"
	supDesechables = "Desechables y empaques"
	supHielo       = "Hielo"
	supLicores     = "Licores"
	supCervezas    = "Cervezas"
	supRepuestos   = "Repuestos y accesorios"
	supHerramienta = "Herramientas"
	supQuimicos    = "Químicos y tintes"
	supAseo        = "Productos de aseo"
	supLenceria    = "Lencería y toallas"
	supPapel       = "Papel y tinta"
	supMateriales  = "Materiales"
	supCombustible = "Combustible"
)

// sectionTemplates por subcategoría (el nombre exacto de defaultSubcategories). Lo aprobó Giovanni en el documento
// «Empre · Categorías y secciones desde cero» (2026-10-28). Para cambiar una plantilla basta editarla aquí: se
// recalcula al arrancar.
var sectionTemplates = map[string]sectionTemplate{
	// Restaurantes
	"Corrientazo y almuerzos": {[]string{"Almuerzos del día", "Sopas", "Platos a la carta", "Bebidas", "Adicionales"}, []string{supCarnes, supVerduras, supGranos, supBebidas, supDesechables}},
	"Comida rápida":           {[]string{"Hamburguesas", "Perros calientes", "Salchipapas", "Bebidas", "Adicionales"}, []string{supPanes, supCarnes, "Salsas y condimentos", supBebidas, supDesechables}},
	"Comida típica":           {[]string{"Desayunos", "Fritos", "Platos típicos", "Bebidas", "Postres"}, []string{supCarnes, supHarinas, supVerduras, "Aceites", supDesechables}},
	"Pescados y mariscos":     {[]string{"Pescados", "Mariscos", "Arroces", "Entradas", "Bebidas"}, []string{supPescados, supVerduras, supGranos, supBebidas, supDesechables}},
	"Asaderos y pollo":        {[]string{"Pollo asado", "Carnes asadas", "Acompañamientos", "Bebidas"}, []string{supCarnes, "Gas y carbón", supVerduras, supBebidas, supDesechables}},
	"Comida internacional":    {[]string{"Entradas", "Platos fuertes", "Pastas", "Bebidas", "Postres"}, []string{supCarnes, supVerduras, supLacteos, supGranos, supDesechables}},
	"Saludable y vegetariana": {[]string{"Bowls y ensaladas", "Platos fuertes", "Jugos y batidos", "Snacks"}, []string{supVerduras, supGranos, supCarnes, supDesechables}},
	// Cafeterías, panaderías y postres
	"Cafetería":            {[]string{"Cafés", "Bebidas frías", "Desayunos", "Panes y pasteles", "Snacks"}, []string{"Café y té", supLacteos, supPanes, supDesechables}},
	"Panadería":            {[]string{"Panes", "Pasteles y tortas", "Galletas", "Bebidas", "Desayunos"}, []string{supHarinas, supLacteos, supGranos, supDesechables}},
	"Repostería y postres": {[]string{"Tortas", "Postres", "Galletas", "Pedidos especiales"}, []string{supHarinas, supLacteos, "Chocolates y rellenos", supDesechables}},
	"Heladería":            {[]string{"Helados", "Malteadas", "Postres", "Adicionales"}, []string{"Helados y bases", supLacteos, "Toppings", supDesechables}},
	"Jugos y fruterías":    {[]string{"Jugos", "Frutas y salpicón", "Batidos", "Snacks"}, []string{supVerduras, supLacteos, supHielo, supDesechables}},
	// Bares y vida nocturna
	"Bar":             {[]string{"Cervezas", "Cócteles", "Licores", "Sin alcohol", "Para picar"}, []string{supLicores, supCervezas, supBebidas, supHielo, supDesechables}},
	"Discoteca":       {[]string{"Entradas", "Botellas", "Cervezas", "Cócteles", "Sin alcohol"}, []string{supLicores, supCervezas, supBebidas, supHielo, supDesechables}},
	"Karaoke":         {[]string{"Salas", "Cervezas", "Licores", "Cócteles", "Para picar"}, []string{supLicores, supCervezas, supBebidas, supHielo, supDesechables}},
	"Billar y juegos": {[]string{"Mesas por hora", "Cervezas", "Bebidas", "Para picar"}, []string{supCervezas, supBebidas, supRepuestos}},
	// Tiendas y comercio
	"Tienda de barrio y minimercado": {[]string{"Granos y abarrotes", "Bebidas", "Lácteos y huevos", "Carnes y pollo", "Snacks y dulces", "Aseo del hogar", "Cuidado personal"}, []string{supDesechables, "Aseo del local"}},
	"Fruver":                         {[]string{"Frutas", "Verduras", "Hierbas y especias", "Granos"}, []string{supDesechables}},
	"Licorera":                       {[]string{"Cervezas", "Aguardiente y ron", "Otros licores", "Gaseosas y aguas", "Snacks", "Hielo"}, []string{supDesechables, supHielo}},
	// Qué es la prenda; para quién es (hombre, mujer, niños) va aparte, en service_items.audience.
	"Ropa y calzado":       {[]string{"Camisetas", "Camisas y blusas", "Pantalones", "Vestidos", "Ropa interior", "Calzado", "Accesorios"}, []string{supDesechables, "Aseo del local"}},
	"Artesanías y regalos": {[]string{"Artesanías", "Regalos", "Decoración", "Recordatorios"}, []string{supMateriales, supDesechables}},
	"Papelería":            {[]string{"Útiles escolares", "Oficina", "Impresiones y copias", "Regalos"}, []string{supPapel, supDesechables}},
	"Ferretería":           {[]string{"Herramientas", "Eléctricos", "Plomería", "Pinturas", "Construcción"}, []string{supDesechables}},
	// Tecnología
	"Celulares y accesorios":     {[]string{"Celulares", "Accesorios", "Cargadores y cables", "Audio"}, []string{supDesechables}},
	"Reparación de celulares":    {[]string{"Pantallas", "Baterías", "Software", "Accesorios"}, []string{supRepuestos, supHerramienta}},
	"Reparación de computadores": {[]string{"Mantenimiento", "Reparaciones", "Software", "Accesorios"}, []string{supRepuestos, supHerramienta}},
	// Belleza y peluquería
	"Peluquería":    {[]string{"Cortes", "Color", "Tratamientos", "Peinados"}, []string{supQuimicos, supDesechables, supHerramienta}},
	"Barbería":      {[]string{"Cortes", "Barba", "Cejas", "Tratamientos"}, []string{supQuimicos, supDesechables, supHerramienta}},
	"Spa y masajes": {[]string{"Masajes", "Faciales", "Corporales", "Paquetes"}, []string{"Aceites y cremas", supLenceria, supDesechables}},
	"Uñas":          {[]string{"Manicure", "Pedicure", "Uñas acrílicas", "Decoración"}, []string{"Esmaltes", supDesechables, supHerramienta}},
	// Salud y bienestar
	"Consultorio médico":    {[]string{"Consultas", "Procedimientos", "Exámenes", "Certificados"}, []string{"Insumos médicos", supDesechables}},
	"Odontología":           {[]string{"Consultas", "Limpieza", "Ortodoncia", "Estética dental", "Urgencias"}, []string{"Insumos médicos", supDesechables}},
	"Droguería":             {[]string{"Medicamentos", "Cuidado personal", "Bebés", "Primeros auxilios", "Vitaminas"}, []string{supDesechables}},
	"Psicología y terapias": {[]string{"Consultas", "Terapias", "Evaluaciones"}, []string{supPapel}},
	"Óptica":                {[]string{"Monturas", "Lentes", "Exámenes de la vista", "Accesorios"}, []string{supRepuestos}},
	// Servicios del hogar
	"Plomería":     {[]string{"Reparaciones", "Instalaciones", "Destapes", "Mantenimiento"}, []string{supRepuestos, supHerramienta}},
	"Electricidad": {[]string{"Instalaciones", "Reparaciones", "Mantenimiento"}, []string{supMateriales, supHerramienta}},
	"Limpieza":     {[]string{"Hogar", "Oficinas", "Muebles y colchones", "Por horas"}, []string{supAseo, supHerramienta}},
	"Jardinería":   {[]string{"Mantenimiento", "Diseño", "Poda"}, []string{"Plantas y abonos", supHerramienta}},
	"Mudanzas":     {[]string{"Mudanzas", "Acarreos", "Embalaje"}, []string{supDesechables, supCombustible}},
	"Fumigación":   {[]string{"Hogar", "Negocios", "Control de plagas"}, []string{supQuimicos, supHerramienta}},
	// Turismo y hospedaje
	"Hotel":               {[]string{"Habitaciones", "Planes", "Alimentación", "Servicios extra"}, []string{supAseo, supLenceria, "Amenities"}},
	"Hostal":              {[]string{"Habitaciones", "Planes", "Alimentación", "Servicios extra"}, []string{supAseo, supLenceria, "Amenities"}},
	"Tours y excursiones": {[]string{"Islas", "City tour", "Pasadías", "Traslados"}, []string{supCombustible, supBebidas}},
	"Alquiler vacacional": {[]string{"Apartamentos", "Casas", "Servicios extra"}, []string{supAseo, supLenceria}},
	// Educación
	"Academia o instituto": {[]string{"Cursos", "Talleres", "Inscripciones"}, []string{supPapel, supMateriales}},
	"Refuerzo escolar":     {[]string{"Matemáticas y ciencias", "Lectura y escritura", "Preicfes", "Tareas"}, []string{supPapel, supMateriales}},
	"Idiomas":              {[]string{"Inglés", "Otros idiomas", "Cursos virtuales"}, []string{supPapel, supMateriales}},
	// Automotriz
	"Taller mecánico":        {[]string{"Mecánica", "Electricidad", "Frenos y suspensión", "Mantenimiento"}, []string{supRepuestos, "Aceites y lubricantes", supHerramienta}},
	"Lavadero":               {[]string{"Lavado", "Polichado", "Motos", "Tapicería"}, []string{supAseo, supHerramienta}},
	"Repuestos y accesorios": {[]string{"Repuestos de carro", "Repuestos de moto", "Llantas", "Aceites", "Accesorios"}, []string{supDesechables}},
	"Alquiler de vehículos":  {[]string{"Carros", "Motos", "Con conductor"}, []string{supCombustible, supAseo}},
	// Mascotas
	"Veterinaria":        {[]string{"Consultas", "Vacunas", "Cirugías", "Laboratorio", "Tienda"}, []string{"Medicamentos veterinarios", supDesechables}},
	"Peluquería canina":  {[]string{"Baño", "Corte", "Spa"}, []string{supAseo, supDesechables}},
	"Tienda de mascotas": {[]string{"Alimento", "Accesorios", "Aseo e higiene", "Juguetes"}, []string{supDesechables}},
	"Guardería y paseos": {[]string{"Guardería", "Paseos", "Hospedaje"}, []string{"Alimento para mascotas", supAseo}},
	// Eventos y fiestas
	"Decoración y alquiler":   {[]string{"Decoración", "Alquiler de mobiliario", "Globos", "Paquetes"}, []string{supMateriales}},
	"Catering":                {[]string{"Menús", "Pasabocas", "Bebidas", "Meseros"}, []string{supCarnes, supVerduras, supGranos, supDesechables}},
	"DJ y sonido":             {[]string{"DJ", "Sonido", "Luces", "Paquetes"}, []string{supRepuestos}},
	"Organización de eventos": {[]string{"Bodas", "Cumpleaños", "Empresariales", "Paquetes"}, []string{supMateriales}},
	// Fotografía y diseño
	"Fotografía y video":     {[]string{"Sesiones", "Eventos", "Video", "Impresión de fotos"}, []string{supPapel}},
	"Diseño gráfico":         {[]string{"Logos", "Redes sociales", "Piezas impresas", "Páginas web"}, []string{supPapel}},
	"Impresión y publicidad": {[]string{"Impresiones", "Pendones y avisos", "Tarjetas", "Estampados"}, []string{supPapel, supMateriales}},
	// Deporte y recreación
	"Gimnasio y crossfit":              {[]string{"Mensualidades", "Días sueltos", "Clases", "Suplementos"}, []string{supAseo, supBebidas}},
	"Entrenadores y clases deportivas": {[]string{"Clases personalizadas", "Clases grupales", "Planes"}, []string{supMateriales}},
	"Alquiler de canchas":              {[]string{"Canchas por hora", "Torneos", "Bebidas"}, []string{supBebidas, supAseo}},
	"Tienda deportiva":                 {[]string{"Ropa deportiva", "Calzado", "Implementos", "Suplementos"}, []string{supDesechables}},
	// Otros: solo «Otros», que todo negocio ya tiene.
	"Otros": {nil, nil},
}

// SeedSections crea las secciones que falten en el catálogo global y rehace la plantilla de cada subcategoría
// (subcategory_sections) según sectionTemplates. Idempotente: corre en cada arranque, así un cambio de plantilla
// llega a todos los negocios sin migrar nada. Una sección que salió de las plantillas solo se borra si nadie la usa.
func SeedSections(db *gorm.DB) error {
	// Crea las tablas si aún no existen: así también funciona desde los comandos de reinicio (resetcatalog, resetall),
	// que no corren el AutoMigrate del backend.
	if err := db.AutoMigrate(&models.Section{}, &models.SubcategorySection{}, &models.EntitySection{}, &models.SectionRequest{}); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		ids := map[string]map[string]models.Section{models.SectionSell: {}, models.SectionSupply: {}}
		ensure := func(kind, name string) (models.Section, error) {
			if s, ok := ids[kind][name]; ok {
				return s, nil
			}
			s := models.Section{Kind: kind, Name: name, Icon: SectionIcon(kind, name)}
			if err := tx.Where("kind = ? AND name = ?", kind, name).FirstOrCreate(&s).Error; err != nil {
				return s, err
			}
			if want := SectionIcon(kind, name); s.Icon != want {
				s.Icon = want
				if err := tx.Model(&models.Section{}).Where("id = ?", s.ID).Update("icon", want).Error; err != nil {
					return s, err
				}
			}
			ids[kind][name] = s
			return s, nil
		}
		for _, k := range []string{models.SectionSell, models.SectionSupply} {
			if _, err := ensure(k, models.SectionOther); err != nil {
				return err
			}
		}
		// Todo el catálogo existe aunque falte alguna subcategoría: «Más secciones» lo busca entero.
		for _, tpl := range sectionTemplates {
			for _, n := range tpl.Sell {
				if _, err := ensure(models.SectionSell, n); err != nil {
					return err
				}
			}
			for _, n := range tpl.Supply {
				if _, err := ensure(models.SectionSupply, n); err != nil {
					return err
				}
			}
		}
		var subs []models.Subcategory
		if err := tx.Find(&subs).Error; err != nil {
			return err
		}
		missing := 0
		for _, sub := range subs {
			tpl, ok := sectionTemplates[sub.Name]
			if !ok {
				missing++
				continue
			}
			if err := tx.Where("subcategory_id = ?", sub.ID).Delete(&models.SubcategorySection{}).Error; err != nil {
				return err
			}
			rows := make([]models.SubcategorySection, 0, len(tpl.Sell)+len(tpl.Supply))
			for _, part := range []struct {
				kind  string
				names []string
			}{{models.SectionSell, tpl.Sell}, {models.SectionSupply, tpl.Supply}} {
				for i, n := range part.names {
					s, err := ensure(part.kind, n)
					if err != nil {
						return err
					}
					rows = append(rows, models.SubcategorySection{SubcategoryID: sub.ID, SectionID: s.ID, Position: i})
				}
			}
			if len(rows) > 0 {
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
			}
		}
		if missing > 0 {
			log.Printf("Secciones: %d subcategorías sin plantilla (sus negocios solo tendrán «Otros» y lo que agreguen)", missing)
		}
		// Quita del catálogo las secciones que salieron de las plantillas y nadie usa (ni un ítem, ni un negocio, ni una
		// petición): así no aparecen en «Más secciones». Una que algún ítem use se queda, y las de las plantillas también,
		// aunque hoy no exista su subcategoría.
		keep := []uuid.UUID{}
		for _, byName := range ids {
			for _, s := range byName {
				keep = append(keep, s.ID)
			}
		}
		q := "DELETE FROM sections s WHERE s.name <> ? AND s.id NOT IN ?"
		for _, t := range []string{"subcategory_sections", "entity_sections", "service_items", "stock_items", "section_requests"} {
			if !tx.Migrator().HasTable(t) {
				continue
			}
			if !tx.Migrator().HasColumn(t, "section_id") {
				continue
			}
			q += " AND NOT EXISTS (SELECT 1 FROM " + t + " x WHERE x.section_id = s.id)"
		}
		res := tx.Exec(q, models.SectionOther, keep)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Printf("Secciones: %d sin uso quitadas del catálogo", res.RowsAffected)
		}
		// Las que no vienen de plantillas (las que agregó el equipo desde las peticiones) llevan el ícono genérico de su
		// tipo si aún no tienen uno.
		for _, k := range []string{models.SectionSell, models.SectionSupply} {
			if err := tx.Model(&models.Section{}).Where("kind = ? AND icon = ''", k).Update("icon", SectionIcon(k, "")).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
