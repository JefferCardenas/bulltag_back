package main

import (
	"errors"
	"os"
)

// Config guarda la configuración del servicio. Se lee de variables de entorno,
// que es la forma estándar de configurar servicios en Docker y en la nube
// (así no metes contraseñas dentro del código).
type Config struct {
	Port        string // puerto HTTP donde escucha el servicio
	DatabaseURL string // cadena de conexión a PostgreSQL
	APIToken    string // token secreto que ChirpStack debe enviar en cada petición
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIToken:    os.Getenv("INGEST_TOKEN"),
	}

	// En Go los errores son valores normales que se devuelven y se revisan,
	// no hay try/catch como en Java.
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("falta la variable de entorno DATABASE_URL")
	}
	if cfg.APIToken == "" {
		return cfg, errors.New("falta la variable de entorno INGEST_TOKEN")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
