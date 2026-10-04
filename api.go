package main

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Estructuras para armar GeoJSON, el formato estándar para datos geográficos
// que entienden Leaflet, QGIS, Google Maps, etc.
// OJO: en GeoJSON las coordenadas van como [longitud, latitud].
type geoPoint struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"`
}

type geoFeature struct {
	Type       string         `json:"type"`
	Geometry   geoPoint       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type featureCollection struct {
	Type     string       `json:"type"`
	Features []geoFeature `json:"features"`
}

// LivePositions: GET /v1/positions/live
// Devuelve la última posición de todos los animales (para el mapa en vivo).
func (h *Handler) LivePositions(w http.ResponseWriter, r *http.Request) {
	positions, err := h.store.LivePositions(r.Context())
	if err != nil {
		h.log.Error("error consultando posiciones en vivo", "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}

	fc := featureCollection{
		Type:     "FeatureCollection",
		Features: make([]geoFeature, 0, len(positions)),
	}
	for _, p := range positions {
		fc.Features = append(fc.Features, geoFeature{
			Type:     "Feature",
			Geometry: geoPoint{Type: "Point", Coordinates: [2]float64{p.Lon, p.Lat}},
			Properties: map[string]any{
				"animal_id":   p.AnimalID,
				"tag":         p.Tag,
				"dev_eui":     p.DevEUI,
				"battery":     p.Battery,
				"recorded_at": p.RecordedAt,
			},
		})
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, fc)
}

// ListAnimals: GET /v1/animals
func (h *Handler) ListAnimals(w http.ResponseWriter, r *http.Request) {
	animals, err := h.store.ListAnimals(r.Context())
	if err != nil {
		h.log.Error("error listando animales", "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, animals)
}

// Track: GET /v1/animals/{id}/track?from=...&to=...&limit=...
// Fechas en formato RFC3339 con Z, ej. 2026-10-03T15:04:05Z.
// Por defecto: últimas 24 horas, máximo 2000 puntos (tope 10000).
func (h *Handler) Track(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id inválido", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()

	to, err := parseTimeParam(q, "to", time.Now().UTC())
	if err != nil {
		http.Error(w, "parámetro 'to' inválido, usa RFC3339 (ej. 2026-10-03T15:04:05Z)", http.StatusBadRequest)
		return
	}
	from, err := parseTimeParam(q, "from", to.Add(-24*time.Hour))
	if err != nil {
		http.Error(w, "parámetro 'from' inválido, usa RFC3339 (ej. 2026-10-03T15:04:05Z)", http.StatusBadRequest)
		return
	}
	if !from.Before(to) {
		http.Error(w, "'from' debe ser anterior a 'to'", http.StatusBadRequest)
		return
	}

	limit := 2000
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit inválido", http.StatusBadRequest)
			return
		}
		if n > 10000 {
			n = 10000
		}
		limit = n
	}

	points, err := h.store.Track(r.Context(), id, from, to, limit)
	if err != nil {
		h.log.Error("error consultando recorrido", "animal_id", id, "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"animal_id": id,
		"from":      from,
		"to":        to,
		"count":     len(points),
		"points":    points,
	})
}

func parseTimeParam(q url.Values, name string, def time.Time) (time.Time, error) {
	v := q.Get(name)
	if v == "" {
		return def, nil
	}
	return time.Parse(time.RFC3339, v)
}
