// Simulador de vacas: envía al ingest-service mensajes iguales a los que
// mandaría ChirpStack, para probar el sistema sin collares ni gateway.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type cow struct {
	eui     string
	name    string
	lat     float64
	lon     float64
	heading float64 // dirección de caminata en radianes
	battery int
	fcnt    uint32
	ticks   int
}

func main() {
	url := getEnv("INGEST_URL", "http://localhost:8080/v1/ingest/chirpstack?event=up")
	token := os.Getenv("INGEST_TOKEN")
	if token == "" {
		log.Fatal("falta la variable INGEST_TOKEN")
	}
	numCows := getEnvInt("COWS", 5)
	interval := getEnvDuration("INTERVAL", 5*time.Second)
	backfill := getEnvInt("BACKFILL_POINTS", 60)
	centerLat := getEnvFloat("CENTER_LAT", 3.982274947576973)
	centerLon := getEnvFloat("CENTER_LON", -73.78320435338192)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	runID := time.Now().Unix() // distingue ejecuciones para no repetir deduplicationId

	cows := make([]*cow, numCows)
	for i := range cows {
		cows[i] = &cow{
			eui:     fmt.Sprintf("a0b0c0d0e0f0%04x", i+1), // 16 caracteres hexadecimales
			name:    fmt.Sprintf("vaca-sim-%02d", i+1),
			lat:     centerLat + (rand.Float64()-0.5)*0.004,
			lon:     centerLon + (rand.Float64()-0.5)*0.004,
			heading: rand.Float64() * 2 * math.Pi,
			battery: 60 + rand.Intn(41),
		}
	}

	// 1) Historial: genera 1 punto por minuto hacia atrás, para que los
	//    recorridos ya tengan datos al abrir el mapa.
	if backfill > 0 {
		log.Printf("generando %d puntos de historial para %d vacas...", backfill, numCows)
		now := time.Now().UTC()
		for step := backfill; step >= 1; step-- {
			if ctx.Err() != nil {
				return
			}
			ts := now.Add(-time.Duration(step) * time.Minute)
			for _, c := range cows {
				c.move(centerLat, centerLon)
				if err := send(client, url, token, runID, c, ts); err != nil {
					log.Fatalf("no se pudo enviar al ingest-service: %v", err)
				}
			}
		}
	}

	// 2) En vivo: un mensaje por vaca cada INTERVAL.
	log.Printf("simulando en vivo, un mensaje por vaca cada %s (Ctrl+C para detener)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("simulador detenido")
			return
		case <-ticker.C:
			for _, c := range cows {
				c.move(centerLat, centerLon)
				if err := send(client, url, token, runID, c, time.Now().UTC()); err != nil {
					log.Printf("error enviando %s: %v", c.name, err)
				}
			}
			log.Printf("enviadas %d posiciones", len(cows))
		}
	}
}

// move simula una caminata suave: la vaca mantiene su dirección con pequeños
// cambios, y rebota al llegar al borde del "potrero" (~550 m del centro).
func (c *cow) move(centerLat, centerLon float64) {
	const maxOffset = 0.005 // grados, ≈ 550 m

	c.heading += (rand.Float64() - 0.5) * 0.8
	stepMeters := 3 + rand.Float64()*10

	c.lat += math.Cos(c.heading) * stepMeters / 111320.0
	c.lon += math.Sin(c.heading) * stepMeters / (111320.0 * math.Cos(c.lat*math.Pi/180))

	outLat := c.lat < centerLat-maxOffset || c.lat > centerLat+maxOffset
	outLon := c.lon < centerLon-maxOffset || c.lon > centerLon+maxOffset
	if outLat || outLon {
		c.heading += math.Pi // dar la vuelta
	}
	c.lat = clamp(c.lat, centerLat-maxOffset, centerLat+maxOffset)
	c.lon = clamp(c.lon, centerLon-maxOffset, centerLon+maxOffset)

	c.ticks++
	if c.ticks%20 == 0 && c.battery > 5 {
		c.battery--
	}
}

// encodePayload arma los 10 bytes con el mismo formato que describe decoder.go.
func encodePayload(lat, lon float64, battery int) []byte {
	b := make([]byte, 10)
	binary.BigEndian.PutUint32(b[0:4], uint32(int32(math.Round(lat*1e6))))
	binary.BigEndian.PutUint32(b[4:8], uint32(int32(math.Round(lon*1e6))))
	b[8] = byte(battery)
	b[9] = 0x01 // GPS con fix
	return b
}

func send(client *http.Client, url, token string, runID int64, c *cow, ts time.Time) error {
	c.fcnt++
	body := map[string]any{
		"deduplicationId": fmt.Sprintf("sim-%d-%s-%d", runID, c.eui, c.fcnt),
		"time":            ts.Format(time.RFC3339Nano),
		"deviceInfo":      map[string]string{"deviceName": c.name, "devEui": c.eui},
		"fCnt":            c.fcnt,
		"fPort":           1,
		"data":            base64.StdEncoding.EncodeToString(encodePayload(c.lat, c.lon, c.battery)),
		"rxInfo":          []map[string]any{{"rssi": -70 - rand.Intn(40), "snr": 5 + rand.Float64()*5}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("el servicio respondió %d", resp.StatusCode)
	}
	return nil
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
