package main

import (
	"log"
	"net/http"
	"time"

	"github.com/ejuju/boltdb_webgui/internal"
)

func main() {
	server := internal.NewServer("test.boltdb")
	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           server.NewHTTPHandler(),
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 30 * time.Second,
		MaxHeaderBytes:    8000,
	}

	log.Printf("starting HTTP server (%s)", httpServer.Addr)
	err := httpServer.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
