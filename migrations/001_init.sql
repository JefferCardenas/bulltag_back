-- Activa PostGIS (tipos y funciones geoespaciales) en esta base de datos.
CREATE EXTENSION IF NOT EXISTS postgis;

-- Un registro por collar/animal. Se crea solo la primera vez que llega
-- un mensaje de un dev_eui nuevo.
CREATE TABLE IF NOT EXISTS animals (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dev_eui    TEXT        NOT NULL UNIQUE,
    tag        TEXT        NOT NULL,          -- nombre legible, ej. "vaca-001"
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Historial de posiciones.
-- geography(Point, 4326): punto sobre la esfera terrestre en WGS84 (GPS).
-- Las distancias que calcules con este tipo salen en metros.
CREATE TABLE IF NOT EXISTS positions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    animal_id     BIGINT      NOT NULL REFERENCES animals(id),
    recorded_at   TIMESTAMPTZ NOT NULL,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    geom          geography(Point, 4326) NOT NULL,
    battery       SMALLINT,
    rssi          SMALLINT,
    snr           REAL,
    frame_counter BIGINT,
    dedup_id      TEXT        NOT NULL UNIQUE  -- evita guardar el mismo mensaje dos veces
);

-- Para pedir rápido "las posiciones de la vaca X entre estas fechas".
CREATE INDEX IF NOT EXISTS positions_animal_time_idx
    ON positions (animal_id, recorded_at DESC);

-- Índice espacial: acelera consultas tipo "qué hay dentro de este potrero".
CREATE INDEX IF NOT EXISTS positions_geom_idx
    ON positions USING GIST (geom);

-- Solo la última posición de cada animal: es lo que consulta el mapa en vivo.
CREATE TABLE IF NOT EXISTS last_positions (
    animal_id   BIGINT PRIMARY KEY REFERENCES animals(id),
    recorded_at TIMESTAMPTZ NOT NULL,
    geom        geography(Point, 4326) NOT NULL,
    battery     SMALLINT
);
