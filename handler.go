package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const maxBodyBytes = 1 << 20 // 1 MB: más que suficiente para un uplink

// uplinkEvent es la parte del JSON de ChirpStack v4 que nos interesa.
// Las etiquetas `json:"..."` indican cómo se llama cada campo en el JSON.
// Los campos que no declaramos aquí simplemente se ignoran.
type uplinkEvent struct {
	DeduplicationID string    `json:"deduplicationId"`
	Time            time.Time `json:"time"`
	DeviceInfo      struct {
		DeviceName string `json:"deviceName"`
		DevEui     string `json:"devEui"`
	} `json:"deviceInfo"`
	FCnt   uint32   `json:"fCnt"`
	FPort  uint8    `json:"fPort"`
	Data   string   `json:"data"` // payload binario codificado en base64
	RxInfo []rxInfo `json:"rxInfo"`
}

// rxInfo describe cómo cada gateway escuchó el mensaje.
type rxInfo struct {
	Rssi int     `json:"rssi"`
	Snr  float64 `json:"snr"`
}

type Handler struct {
	store *Store
	token string
	log   *slog.Logger
}

// ChirpStackUplink recibe los eventos que ChirpStack envía por HTTP.
func (h *Handler) ChirpStackUplink(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}

	// ChirpStack envía distintos eventos (up, join, status, log...) a la
	// misma URL, indicando el tipo en ?event=. Solo procesamos "up".
	if r.URL.Query().Get("event") != "up" {
		w.WriteHeader(http.StatusOK)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var ev uplinkEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		h.log.Warn("JSON inválido", "error", err)
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}
	if ev.DeviceInfo.DevEui == "" || ev.DeduplicationID == "" {
		http.Error(w, "faltan deviceInfo.devEui o deduplicationId", http.StatusBadRequest)
		return
	}

	raw, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil {
		h.log.Warn("data no es base64 válido", "devEui", ev.DeviceInfo.DevEui, "error", err)
		http.Error(w, "data no es base64 válido", http.StatusBadRequest)
		return
	}

	decoded, err := DecodePayload(raw)
	if err != nil {
		h.log.Warn("payload inválido", "devEui", ev.DeviceInfo.DevEui, "error", err)
		http.Error(w, "payload inválido: "+err.Error(), http.StatusBadRequest)
		return
	}

	if !decoded.HasFix {
		// El collar no tenía señal GPS. No guardamos posición, pero respondemos
		// 200 para que ChirpStack no lo trate como un error.
		h.log.Info("mensaje sin fix GPS", "devEui", ev.DeviceInfo.DevEui, "battery", decoded.Battery)
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_fix"})
		return
	}

	// Usamos la hora que ChirpStack asigna al mensaje.
	recordedAt := ev.Time
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	rssi, snr := bestSignal(ev.RxInfo)

	inserted, err := h.store.SavePosition(r.Context(), Position{
		DevEUI:     ev.DeviceInfo.DevEui,
		DeviceName: ev.DeviceInfo.DeviceName,
		DedupID:    ev.DeduplicationID,
		RecordedAt: recordedAt,
		Lat:        decoded.Lat,
		Lon:        decoded.Lon,
		Battery:    decoded.Battery,
		RSSI:       rssi,
		SNR:        snr,
		FCnt:       int64(ev.FCnt),
	})
	if err != nil {
		h.log.Error("error guardando posición", "devEui", ev.DeviceInfo.DevEui, "error", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}

	status := "stored"
	if !inserted {
		status = "duplicate"
	}
	h.log.Info("uplink procesado",
		"status", status,
		"device", ev.DeviceInfo.DeviceName,
		"lat", decoded.Lat,
		"lon", decoded.Lon,
		"battery", decoded.Battery,
	)
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// Health sirve para saber si el servicio y la base de datos están vivos.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// authorized compara el header "Authorization: Bearer <token>" con el token
// configurado. ConstantTimeCompare evita ataques que adivinan el token
// midiendo cuánto tarda la comparación.
func (h *Handler) authorized(r *http.Request) bool {
	got := r.Header.Get("Authorization")
	want := "Bearer " + h.token
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// bestSignal devuelve el RSSI/SNR del gateway que mejor escuchó el mensaje.
func bestSignal(rx []rxInfo) (int, float64) {
	if len(rx) == 0 {
		return 0, 0
	}
	best := rx[0]
	for _, r := range rx[1:] {
		if r.Rssi > best.Rssi {
			best = r
		}
	}
	return best.Rssi, best.Snr
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
