package main

import (
	"encoding/binary"
	"fmt"
)

// FORMATO DEL PAYLOAD LORA (10 bytes, big-endian)
//
//	Bytes 0-3 : latitud  × 1.000.000, entero de 32 bits CON signo
//	Bytes 4-7 : longitud × 1.000.000, entero de 32 bits CON signo
//	Byte  8   : batería en porcentaje (0-100)
//	Byte  9   : flags. Bit 0 = 1 si el GPS tiene "fix" (posición válida)
//
// Ejemplo: latitud 4.142000 se envía como el entero 4142000.
// Con 6 decimales la precisión es de ~11 cm, de sobra para ganado.
//
// El firmware del ESP32 debe armar el mensaje exactamente así.
const PayloadSize = 10

// DecodedPayload es el contenido ya "traducido" del mensaje binario.
type DecodedPayload struct {
	Lat     float64
	Lon     float64
	Battery int
	HasFix  bool
}

func DecodePayload(data []byte) (DecodedPayload, error) {
	if len(data) != PayloadSize {
		return DecodedPayload{}, fmt.Errorf("payload de %d bytes, se esperaban %d", len(data), PayloadSize)
	}

	// Leemos 4 bytes como uint32 y los reinterpretamos como int32 para
	// que los valores negativos (longitudes al oeste) salgan bien.
	latRaw := int32(binary.BigEndian.Uint32(data[0:4]))
	lonRaw := int32(binary.BigEndian.Uint32(data[4:8]))

	p := DecodedPayload{
		Lat:     float64(latRaw) / 1e6,
		Lon:     float64(lonRaw) / 1e6,
		Battery: int(data[8]),
		HasFix:  data[9]&0x01 == 1,
	}

	if p.Battery > 100 {
		return DecodedPayload{}, fmt.Errorf("batería fuera de rango: %d", p.Battery)
	}

	// Sin fix, lat/lon no son confiables (suelen venir en 0), así que solo
	// validamos el rango cuando el GPS dice que la posición es válida.
	if p.HasFix {
		if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return DecodedPayload{}, fmt.Errorf("coordenadas fuera de rango: lat=%f lon=%f", p.Lat, p.Lon)
		}
	}
	return p, nil
}
