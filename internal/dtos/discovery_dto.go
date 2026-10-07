package dtos

// DiscoveryDTO is one ranked discovery result: the map card fields plus the
// computed distance and any active offer.
type DiscoveryDTO struct {
	EntityMapDTO
	DistanceMeters float64        `json:"distance_meters"`
	HasActiveOffer bool           `json:"has_active_offer"`
	ActiveOffer    *OfferResponse `json:"active_offer,omitempty"`
}
