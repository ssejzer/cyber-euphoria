package main

import (
	"log"
	"net/http"
	"strings"
)

func main() {
	fs := http.FileServer(http.Dir("."))
	
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Asegurar el tipo MIME correcto para archivos .wasm
		if strings.HasSuffix(r.URL.Path, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		}
		// Permitir SharedArrayBuffer (opcional, pero recomendado para Wasm moderno)
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		
		fs.ServeHTTP(w, r)
	}))

	log.Println("Servidor iniciado en http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
