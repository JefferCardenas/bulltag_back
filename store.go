package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Position es una lectura de GPS lista para guardarse.
type Position struct {
	DevEUI     string // identificador único del dispositivo LoRa (el "collar")
	DeviceName string // nombre legible configurado en ChirpStack, ej. "vaca-001"
	DedupID    string // ID único del mensaje, evita guardar duplicados
	RecordedAt time.Time
	Lat        float64
	Lon        float64
	Battery    int
	RSSI       int
	SNR        float64
	FCnt       int64
}

// Store encapsula el acceso a la base de datos.
// pgxpool.Pool es un "pool de conexiones": reutiliza conexiones abiertas
// en vez de abrir una nueva por cada petición.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Si el animal ya existe (mismo dev_eui) devuelve su id; si no, lo crea.
// El "DO UPDATE ... SET dev_eui = EXCLUDED.dev_eui" no cambia nada, pero
// obliga a Postgres a devolver el id también cuando la fila ya existía.
const upsertAnimalSQL = `
INSERT INTO animals (dev_eui, tag)
VALUES ($1, $2)
ON CONFLICT (dev_eui) DO UPDATE SET dev_eui = EXCLUDED.dev_eui
RETURNING id`

// IMPORTANTE: ST_MakePoint recibe (LONGITUD, LATITUD), o sea (x, y).
// Es el error más común con PostGIS: el orden es al revés de como se dice.
// 4326 es el SRID de WGS84, el sistema de coordenadas que usa el GPS.
//
// ON CONFLICT (dedup_id) DO NOTHING: si el mensaje ya se guardó, se ignora.
const insertPositionSQL = `
INSERT INTO positions
    (animal_id, recorded_at, geom, battery, rssi, snr, frame_counter, dedup_id)
VALUES
    ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5, $6, $7, $8, $9)
ON CONFLICT (dedup_id) DO NOTHING`

// Guarda la última posición conocida, pero solo si es más reciente que la
// que ya había (por si llegan mensajes desordenados).
const upsertLastPositionSQL = `
INSERT INTO last_positions (animal_id, recorded_at, geom, battery)
VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5)
ON CONFLICT (animal_id) DO UPDATE
SET recorded_at = EXCLUDED.recorded_at,
    geom        = EXCLUDED.geom,
    battery     = EXCLUDED.battery
WHERE last_positions.recorded_at < EXCLUDED.recorded_at`

// SavePosition guarda la posición dentro de una transacción: o se guardan
// todos los pasos o ninguno. Devuelve false si el mensaje era un duplicado.
func (s *Store) SavePosition(ctx context.Context, p Position) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	// Si todo sale bien se hace Commit y este Rollback no hace nada.
	// Si algo falla antes del Commit, deshace los cambios.
	defer tx.Rollback(ctx)

	var animalID int64
	if err := tx.QueryRow(ctx, upsertAnimalSQL, p.DevEUI, p.DeviceName).Scan(&animalID); err != nil {
		return false, err
	}

	tag, err := tx.Exec(ctx, insertPositionSQL,
		animalID, p.RecordedAt, p.Lon, p.Lat, p.Battery, p.RSSI, p.SNR, p.FCnt, p.DedupID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// Duplicado: no hay nada más que guardar.
		return false, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, upsertLastPositionSQL,
		animalID, p.RecordedAt, p.Lon, p.Lat, p.Battery); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
