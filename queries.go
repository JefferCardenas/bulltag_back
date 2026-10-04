package main

import (
	"context"
	"time"
)

// LivePosition es la última posición conocida de un animal.
type LivePosition struct {
	AnimalID   int64
	Tag        string
	DevEUI     string
	Lat        float64
	Lon        float64
	Battery    *int16 // puntero porque en la base de datos puede ser NULL
	RecordedAt time.Time
}

// AnimalSummary es un animal con su última lectura (si tiene).
type AnimalSummary struct {
	ID       int64      `json:"id"`
	Tag      string     `json:"tag"`
	DevEUI   string     `json:"dev_eui"`
	LastSeen *time.Time `json:"last_seen"`
	Battery  *int16     `json:"battery"`
}

// TrackPoint es un punto del recorrido de un animal.
type TrackPoint struct {
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	RecordedAt time.Time `json:"recorded_at"`
	Battery    *int16    `json:"battery"`
}

// ST_Y = latitud y ST_X = longitud. Para leer las coordenadas de un punto
// "geography" hay que convertirlo antes a "geometry" con ::geometry.
const livePositionsSQL = `
SELECT a.id, a.tag, a.dev_eui,
       ST_Y(l.geom::geometry), ST_X(l.geom::geometry),
       l.battery, l.recorded_at
FROM last_positions l
JOIN animals a ON a.id = l.animal_id
ORDER BY a.tag`

const listAnimalsSQL = `
SELECT a.id, a.tag, a.dev_eui, l.recorded_at, l.battery
FROM animals a
LEFT JOIN last_positions l ON l.animal_id = a.id
ORDER BY a.tag`

// Se piden los puntos más recientes primero (DESC) para que, si el rango es
// muy grande, el LIMIT recorte lo más viejo y no lo más nuevo.
const trackSQL = `
SELECT ST_Y(geom::geometry), ST_X(geom::geometry), recorded_at, battery
FROM positions
WHERE animal_id = $1 AND recorded_at >= $2 AND recorded_at <= $3
ORDER BY recorded_at DESC
LIMIT $4`

func (s *Store) LivePositions(ctx context.Context) ([]LivePosition, error) {
	rows, err := s.pool.Query(ctx, livePositionsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// make(..., 0) en vez de "var out []..." para que el JSON salga como []
	// y no como null cuando no hay datos.
	out := make([]LivePosition, 0)
	for rows.Next() {
		var p LivePosition
		if err := rows.Scan(&p.AnimalID, &p.Tag, &p.DevEUI, &p.Lat, &p.Lon, &p.Battery, &p.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListAnimals(ctx context.Context) ([]AnimalSummary, error) {
	rows, err := s.pool.Query(ctx, listAnimalsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AnimalSummary, 0)
	for rows.Next() {
		var a AnimalSummary
		if err := rows.Scan(&a.ID, &a.Tag, &a.DevEUI, &a.LastSeen, &a.Battery); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Track devuelve los puntos de un animal entre from y to, en orden cronológico.
func (s *Store) Track(ctx context.Context, animalID int64, from, to time.Time, limit int) ([]TrackPoint, error) {
	rows, err := s.pool.Query(ctx, trackSQL, animalID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TrackPoint, 0)
	for rows.Next() {
		var p TrackPoint
		if err := rows.Scan(&p.Lat, &p.Lon, &p.RecordedAt, &p.Battery); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// La consulta los trajo del más nuevo al más viejo: los invertimos.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
