package main

import (
	"log"
	"net/http"
)

func main() {
	server := http.NewServeMux()

	server.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: server,
	}

	log.Println("Starting server on :8080")
	if err := httpServer.ListenAndServe(); err != nil {
		panic(err)
	}
}
