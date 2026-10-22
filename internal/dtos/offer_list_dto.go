package dtos

// RelampagoListItem es un relámpago en el listado de la pestaña Ofertas, con
// la distancia y el nombre del negocio/categoría.
type RelampagoListItem struct {
	OfferResponse
	DistanceMeters float64 `json:"distance_meters"`
	EntityName     string  `json:"entity_name"`
	CategoryName   string  `json:"category_name"`
}

// PromotionListItem es una promoción en el listado de la pestaña Ofertas.
type PromotionListItem struct {
	PromotionResponse
	DistanceMeters float64 `json:"distance_meters"`
	EntityName     string  `json:"entity_name"`
	CategoryName   string  `json:"category_name"`
}
