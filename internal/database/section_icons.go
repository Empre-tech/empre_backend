package database

import "empre_backend/internal/models"

// Íconos de las secciones (nombres de Ionicons, versión «outline»). La app muestra el de la sección en su fila, en
// los productos e insumos sin foto y en la hoja para elegir sección; si no conoce un nombre, usa uno genérico. Para
// cambiar uno basta editarlo aquí: se aplica al arrancar.
const (
	defaultSellIcon   = "pricetag-outline"
	defaultSupplyIcon = "cube-outline"
)

// sectionIcons por nombre de sección; vale para los dos tipos salvo que supplyIcons diga otra cosa.
var sectionIcons = map[string]string{
	// Comida y bebida
	"Almuerzos del día": "restaurant-outline", "Sopas": "restaurant-outline", "Platos a la carta": "restaurant-outline",
	"Platos típicos": "restaurant-outline", "Platos fuertes": "restaurant-outline", "Entradas": "restaurant-outline",
	"Menús": "restaurant-outline", "Pasabocas": "restaurant-outline", "Arroces": "restaurant-outline",
	"Pastas": "restaurant-outline", "Acompañamientos": "restaurant-outline", "Bowls y ensaladas": "leaf-outline",
	"Hamburguesas": "fast-food-outline", "Perros calientes": "fast-food-outline", "Salchipapas": "fast-food-outline",
	"Para picar": "fast-food-outline", "Snacks": "fast-food-outline", "Fritos": "flame-outline",
	"Desayunos": "sunny-outline", "Adicionales": "add-circle-outline", "Pollo asado": "flame-outline",
	"Carnes asadas": "flame-outline", "Pescados": "fish-outline", "Mariscos": "fish-outline",
	"Bebidas": "water-outline", "Bebidas frías": "snow-outline", "Sin alcohol": "water-outline",
	"Gaseosas y aguas": "water-outline", "Jugos": "nutrition-outline", "Jugos y batidos": "nutrition-outline",
	"Batidos": "nutrition-outline", "Frutas y salpicón": "nutrition-outline", "Cafés": "cafe-outline",
	"Cervezas": "beer-outline", "Cócteles": "wine-outline", "Licores": "wine-outline", "Botellas": "wine-outline",
	"Aguardiente y ron": "wine-outline", "Otros licores": "wine-outline", "Postres": "ice-cream-outline",
	"Helados": "ice-cream-outline", "Malteadas": "ice-cream-outline", "Tortas": "gift-outline",
	"Pasteles y tortas": "gift-outline", "Pedidos especiales": "gift-outline", "Galletas": "cafe-outline",
	"Panes": "cafe-outline", "Panes y pasteles": "cafe-outline",
	// Tienda
	"Granos y abarrotes": "basket-outline", "Granos": "basket-outline", "Lácteos y huevos": "egg-outline",
	"Carnes y pollo": "restaurant-outline", "Snacks y dulces": "ice-cream-outline", "Aseo del hogar": "sparkles-outline",
	"Cuidado personal": "rose-outline", "Frutas": "nutrition-outline", "Verduras": "leaf-outline",
	"Hierbas y especias": "leaf-outline", "Hielo": "snow-outline", "Útiles escolares": "pencil-outline",
	"Oficina": "document-text-outline", "Impresiones y copias": "print-outline", "Regalos": "gift-outline",
	"Recordatorios": "gift-outline", "Artesanías": "color-palette-outline", "Decoración": "flower-outline",
	"Herramientas": "hammer-outline", "Eléctricos": "flash-outline", "Plomería": "water-outline",
	"Pinturas": "color-fill-outline", "Construcción": "construct-outline",
	// Ropa
	"Camisetas": "shirt-outline", "Camisas y blusas": "shirt-outline", "Pantalones": "shirt-outline",
	"Vestidos": "shirt-outline", "Ropa interior": "shirt-outline", "Ropa deportiva": "shirt-outline",
	"Calzado": "footsteps-outline", "Accesorios": "bag-handle-outline",
	// Tecnología
	"Celulares": "phone-portrait-outline", "Cargadores y cables": "battery-charging-outline", "Audio": "headset-outline",
	"Pantallas": "phone-portrait-outline", "Baterías": "battery-charging-outline", "Software": "laptop-outline",
	// Belleza y salud
	"Cortes": "cut-outline", "Corte": "cut-outline", "Barba": "cut-outline", "Cejas": "eye-outline",
	"Color": "color-palette-outline", "Tratamientos": "sparkles-outline", "Peinados": "sparkles-outline",
	"Masajes": "hand-left-outline", "Faciales": "happy-outline", "Corporales": "body-outline", "Spa": "sparkles-outline",
	"Manicure": "hand-left-outline", "Pedicure": "footsteps-outline", "Uñas acrílicas": "hand-left-outline",
	"Consultas": "medkit-outline", "Procedimientos": "medkit-outline", "Exámenes": "pulse-outline",
	"Certificados": "document-text-outline", "Limpieza": "sparkles-outline", "Ortodoncia": "happy-outline",
	"Estética dental": "happy-outline", "Urgencias": "alert-circle-outline", "Medicamentos": "medical-outline",
	"Bebés": "happy-outline", "Primeros auxilios": "bandage-outline", "Vitaminas": "fitness-outline",
	"Terapias": "heart-outline", "Evaluaciones": "clipboard-outline", "Monturas": "glasses-outline",
	"Lentes": "glasses-outline", "Exámenes de la vista": "eye-outline",
	// Hogar y servicios
	"Reparaciones": "construct-outline", "Instalaciones": "construct-outline", "Destapes": "water-outline",
	"Mantenimiento": "settings-outline", "Hogar": "home-outline", "Oficinas": "business-outline",
	"Muebles y colchones": "bed-outline", "Por horas": "time-outline", "Diseño": "flower-outline",
	"Poda": "leaf-outline", "Mudanzas": "car-outline", "Acarreos": "car-outline", "Embalaje": "cube-outline",
	"Negocios": "business-outline", "Control de plagas": "bug-outline",
	// Turismo y educación
	"Habitaciones": "bed-outline", "Planes": "map-outline", "Alimentación": "restaurant-outline",
	"Servicios extra": "star-outline", "Islas": "boat-outline", "City tour": "map-outline", "Pasadías": "sunny-outline",
	"Traslados": "car-outline", "Apartamentos": "business-outline", "Casas": "home-outline", "Cursos": "school-outline",
	"Talleres": "school-outline", "Inscripciones": "clipboard-outline", "Matemáticas y ciencias": "calculator-outline",
	"Lectura y escritura": "book-outline", "Preicfes": "school-outline", "Tareas": "book-outline",
	"Inglés": "language-outline", "Otros idiomas": "language-outline", "Cursos virtuales": "laptop-outline",
	// Automotriz y mascotas
	"Mecánica": "construct-outline", "Electricidad": "flash-outline", "Frenos y suspensión": "car-outline",
	"Lavado": "water-outline", "Polichado": "sparkles-outline", "Motos": "bicycle-outline", "Tapicería": "car-outline",
	"Repuestos de carro": "car-outline", "Repuestos de moto": "bicycle-outline", "Llantas": "ellipse-outline",
	"Aceites": "water-outline", "Carros": "car-outline", "Con conductor": "person-outline", "Vacunas": "medkit-outline",
	"Cirugías": "medkit-outline", "Laboratorio": "flask-outline", "Tienda": "storefront-outline", "Baño": "water-outline",
	"Alimento": "paw-outline", "Aseo e higiene": "sparkles-outline", "Juguetes": "paw-outline",
	"Guardería": "paw-outline", "Paseos": "paw-outline", "Hospedaje": "bed-outline",
	// Eventos, fotografía y deporte
	"Alquiler de mobiliario": "albums-outline", "Globos": "balloon-outline", "Paquetes": "gift-outline",
	"Meseros": "people-outline", "DJ": "musical-notes-outline", "Sonido": "musical-notes-outline",
	"Luces": "bulb-outline", "Bodas": "heart-outline", "Cumpleaños": "balloon-outline",
	"Empresariales": "business-outline", "Sesiones": "camera-outline", "Eventos": "camera-outline",
	"Video": "videocam-outline", "Impresión de fotos": "image-outline", "Logos": "brush-outline",
	"Redes sociales": "megaphone-outline", "Piezas impresas": "print-outline", "Páginas web": "laptop-outline",
	"Impresiones": "print-outline", "Pendones y avisos": "megaphone-outline", "Tarjetas": "card-outline",
	"Estampados": "shirt-outline", "Mensualidades": "calendar-outline", "Días sueltos": "ticket-outline",
	"Clases": "fitness-outline", "Suplementos": "barbell-outline", "Clases personalizadas": "person-outline",
	"Clases grupales": "people-outline", "Canchas por hora": "football-outline", "Torneos": "trophy-outline",
	"Implementos": "football-outline", "Salas": "mic-outline", "Mesas por hora": "time-outline",
}

// supplyIcons: insumos cuyo ícono cambia respecto al de la sección de venta con el mismo nombre.
var supplyIcons = map[string]string{
	"Carnes y pollo": "restaurant-outline", "Pescados y mariscos": "fish-outline", "Verduras y frutas": "leaf-outline",
	"Granos y abarrotes": "basket-outline", "Lácteos y huevos": "egg-outline", "Panes y masas": "cafe-outline",
	"Harinas": "basket-outline", "Bebidas": "water-outline", "Desechables y empaques": "cube-outline",
	"Hielo": "snow-outline", "Licores": "wine-outline", "Cervezas": "beer-outline",
	"Repuestos y accesorios": "construct-outline", "Herramientas": "hammer-outline",
	"Químicos y tintes": "flask-outline", "Productos de aseo": "sparkles-outline", "Aseo del local": "sparkles-outline",
	"Lencería y toallas": "bed-outline", "Papel y tinta": "print-outline", "Materiales": "construct-outline",
	"Combustible": "speedometer-outline", "Salsas y condimentos": "flask-outline", "Aceites": "water-outline",
	"Gas y carbón": "flame-outline", "Café y té": "cafe-outline", "Chocolates y rellenos": "ice-cream-outline",
	"Helados y bases": "ice-cream-outline", "Toppings": "ice-cream-outline", "Aceites y cremas": "water-outline",
	"Esmaltes": "color-palette-outline", "Insumos médicos": "medkit-outline", "Medicamentos veterinarios": "medkit-outline",
	"Plantas y abonos": "leaf-outline", "Aceites y lubricantes": "water-outline", "Alimento para mascotas": "paw-outline",
	"Amenities": "sparkles-outline",
}

// SectionIcon: el ícono de una sección por su tipo y nombre (uno genérico si no tiene uno propio).
func SectionIcon(kind, name string) string {
	if kind == models.SectionSupply {
		if ic, ok := supplyIcons[name]; ok {
			return ic
		}
	}
	if ic, ok := sectionIcons[name]; ok {
		return ic
	}
	if name == models.SectionOther {
		return "ellipsis-horizontal-circle-outline"
	}
	if kind == models.SectionSupply {
		return defaultSupplyIcon
	}
	return defaultSellIcon
}
