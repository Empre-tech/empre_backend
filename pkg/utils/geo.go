package utils

import "math"

// DistanceMeters returns the great-circle distance between two coordinates in
// meters using the Haversine formula. Used for claim radius validation and
// discovery ranking (a precise distance, unlike the approximate bounding box).
func DistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0

	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }

	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}

// BBoxAround devuelve un rectángulo (min/max lat/lng) alrededor de un punto
// con un radio en metros, aproximado con grados (suficiente para consultas
// por radio; la distancia exacta se afina con Haversine).
func BBoxAround(lat, lng, radiusMeters float64) (minLat, maxLat, minLng, maxLng float64) {
	degLat := radiusMeters / 111000.0
	degLng := degLat / math.Max(math.Cos(lat*math.Pi/180), 0.1)
	return lat - degLat, lat + degLat, lng - degLng, lng + degLng
}
