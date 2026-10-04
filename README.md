# ganado-ingest

Servicio en Go que recibe los mensajes LoRa (vía webhook de ChirpStack),
guarda la posición GPS en PostgreSQL + PostGIS y la muestra en un mapa web.

```
Collar ESP32 --LoRa--> Gateway --> ChirpStack --HTTP POST--> ingest-service --> PostGIS
                                                                   |
                                          Navegador (Leaflet) <----+  API de lectura (GeoJSON)
```

## Estructura

| Archivo | Para qué sirve |
|---|---|
| `main.go` | Arranca el servidor HTTP y registra las rutas |
| `config.go` | Lee la configuración de variables de entorno |
| `handler.go` | Webhook de ChirpStack (escritura) y health check |
| `decoder.go` | Traduce los 10 bytes del collar a latitud, longitud y batería |
| `store.go` | Consultas SQL de escritura |
| `queries.go` | Consultas SQL de lectura (en vivo, animales, recorrido) |
| `api.go` | Endpoints REST de lectura que devuelven JSON/GeoJSON |
| `web.go` + `web/index.html` | Página del mapa (Leaflet + OpenStreetMap), incrustada en el binario |
| `cmd/simulator/main.go` | Simulador de vacas para probar sin hardware |
| `migrations/001_init.sql` | Crea las tablas y activa PostGIS |
| `Dockerfile`, `docker-compose.yml` | Levantan todo con un solo comando |

## Requisitos

Solo **Docker** con Docker Compose. No hace falta instalar Go.

## Arrancar

```bash
cp .env.example .env        # y edita INGEST_TOKEN
docker compose up -d --build
```

Comprobar que responde:

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

## Ver el mapa con vacas simuladas

1. Abre el mapa en el navegador: **http://localhost:8080**
2. En otra terminal, lanza el simulador:

```bash
docker compose run --rm simulator
```

Genera 5 vacas con 60 minutos de historial y después envía una posición por
vaca cada 5 segundos. Detenlo con `Ctrl + C`. En el mapa verás los puntos
moverse; haz clic en una vaca y pulsa **Ver recorrido (24 h)**.

Para cambiar el número de vacas u otros parámetros, edita las variables del
servicio `simulator` en `docker-compose.yml` (`COWS`, `INTERVAL`,
`BACKFILL_POINTS`) o agrega `CENTER_LAT` / `CENTER_LON` para moverlas a otra zona.

## Endpoints

| Método y ruta | Descripción |
|---|---|
| `GET /` | Página del mapa |
| `GET /healthz` | Estado del servicio y la base de datos |
| `POST /v1/ingest/chirpstack?event=up` | Webhook de ChirpStack (requiere `Authorization: Bearer <token>`) |
| `GET /v1/positions/live` | Última posición de cada animal, en GeoJSON |
| `GET /v1/animals` | Lista de animales con última lectura y batería |
| `GET /v1/animals/{id}/track?from=&to=&limit=` | Recorrido de un animal |

Fechas en formato RFC3339 con `Z`, por ejemplo `2026-10-03T15:04:05Z`.
Por defecto el recorrido cubre las últimas 24 horas (máximo 2000 puntos; tope 10000).

```bash
curl http://localhost:8080/v1/positions/live
curl http://localhost:8080/v1/animals
curl "http://localhost:8080/v1/animals/1/track?limit=100"
```

> **Importante:** los endpoints de lectura todavía **no tienen autenticación**.
> Está bien para pruebas locales, pero hay que protegerlos antes de publicar el
> servicio en internet.

## Probar el webhook a mano (sin simulador)

```bash
curl -i -X POST "http://localhost:8080/v1/ingest/chirpstack?event=up" \
  -H "Authorization: Bearer cambia-este-token" \
  -H "Content-Type: application/json" \
  -d '{
    "deduplicationId": "test-0001",
    "time": "2026-10-03T15:04:05+00:00",
    "deviceInfo": { "deviceName": "vaca-001", "devEui": "0102030405060708" },
    "fCnt": 1,
    "fPort": 1,
    "data": "AD8zsPucjBhXAQ==",
    "rxInfo": [ { "rssi": -92, "snr": 7.5 } ]
  }'
```

Respuesta esperada: `{"status":"stored"}`. Si repites el mismo comando
(mismo `deduplicationId`), responde `{"status":"duplicate"}`.

Ver lo guardado directamente en la base de datos:

```bash
docker compose exec db psql -U ganado -d ganado -c "
SELECT a.tag,
       ST_Y(l.geom::geometry) AS lat,
       ST_X(l.geom::geometry) AS lon,
       l.battery,
       l.recorded_at
FROM last_positions l
JOIN animals a ON a.id = l.animal_id;"
```

## Formato del mensaje LoRa (10 bytes)

El firmware del ESP32 debe enviar exactamente esto, en big-endian:

| Bytes | Contenido |
|---|---|
| 0-3 | Latitud × 1.000.000 (entero de 32 bits con signo) |
| 4-7 | Longitud × 1.000.000 (entero de 32 bits con signo) |
| 8 | Batería en % (0-100) |
| 9 | Flags. Bit 0 = 1 si el GPS tiene fix |

## Comandos de Docker útiles

```bash
docker compose ps                  # qué está corriendo
docker compose logs -f ingest      # logs del servicio en vivo
docker compose stop                # detener sin borrar nada
docker compose down                # detener y quitar contenedores (conserva la base de datos)
docker compose down -v             # además BORRA la base de datos
docker compose up -d --build       # reconstruir tras cambiar código
```

## Pruebas unitarias (opcional, requiere Go 1.22+ instalado)

```bash
go mod tidy
go test ./...
```

## Conectar ChirpStack (cuando lo tengas instalado)

En ChirpStack, dentro de la aplicación → *Integrations* → *HTTP*:

- Payload encoding: **JSON**
- Event endpoint URL: `http://<host-del-servicio>:8080/v1/ingest/chirpstack`
- Headers: `Authorization` = `Bearer <tu INGEST_TOKEN>`

## Qué falta (siguientes pasos)

- Instalar ChirpStack y conectarlo al webhook
- Firmware del ESP32
- Potreros (geocercas) y alertas de salida
- Autenticación para la API de lectura
- Retención de datos: borrar o agregar posiciones antiguas
- Despliegue en la VM con HTTPS
