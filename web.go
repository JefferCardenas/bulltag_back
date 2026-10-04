package main

import (
	"embed"
	"io/fs"
	"net/http"
)

// La directiva "go:embed" mete la carpeta web/ dentro del binario al compilar.
// Así el servicio se entrega como un solo archivo, sin copiar el HTML aparte.
//
//go:embed web
var webFiles embed.FS

func webHandler() (http.Handler, error) {
	// Quitamos el prefijo "web/" para que index.html se sirva en "/".
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}
