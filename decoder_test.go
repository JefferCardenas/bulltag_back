package main

import (
	"encoding/binary"
	"math"
	"testing"
)

// buildPayload arma un mensaje igual al que enviaría el ESP32.
func buildPayload(lat, lon float64, battery, flags byte) []byte {
	b := make([]byte, PayloadSize)
	binary.BigEndian.PutUint32(b[0:4], uint32(int32(math.Round(lat*1e6))))
	binary.BigEndian.PutUint32(b[4:8], uint32(int32(math.Round(lon*1e6))))
	b[8] = battery
	b[9] = flags
	return b
}

func TestDecodePayload_Valido(t *testing.T) {
	// Mismos bytes que el ejemplo del README: 4.142, -73.6266, 87%, con fix.
	data := []byte{0x00, 0x3f, 0x33, 0xb0, 0xfb, 0x9c, 0x8c, 0x18, 0x57, 0x01}

	p, err := DecodePayload(data)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if math.Abs(p.Lat-4.142) > 1e-9 {
		t.Errorf("lat = %f, se esperaba 4.142", p.Lat)
	}
	if math.Abs(p.Lon-(-73.6266)) > 1e-9 {
		t.Errorf("lon = %f, se esperaba -73.6266", p.Lon)
	}
	if p.Battery != 87 {
		t.Errorf("batería = %d, se esperaba 87", p.Battery)
	}
	if !p.HasFix {
		t.Error("se esperaba HasFix = true")
	}
}

func TestDecodePayload_TamanoIncorrecto(t *testing.T) {
	if _, err := DecodePayload([]byte{1, 2, 3}); err == nil {
		t.Error("se esperaba error por tamaño incorrecto")
	}
}

func TestDecodePayload_LatitudFueraDeRango(t *testing.T) {
	if _, err := DecodePayload(buildPayload(95.0, 10.0, 50, 1)); err == nil {
		t.Error("se esperaba error por latitud fuera de rango")
	}
}

func TestDecodePayload_SinFixNoValidaCoordenadas(t *testing.T) {
	p, err := DecodePayload(buildPayload(0, 0, 60, 0))
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if p.HasFix {
		t.Error("se esperaba HasFix = false")
	}
}

func TestDecodePayload_BateriaInvalida(t *testing.T) {
	if _, err := DecodePayload(buildPayload(4.1, -73.6, 150, 1)); err == nil {
		t.Error("se esperaba error por batería > 100")
	}
}
