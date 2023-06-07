package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ejuju/boltdb-webgui/internal"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func main() {
	fpath := "test.boltdb"
	port := "8080"
	if len(os.Args) >= 2 {
		fpath = os.Args[1]
	}
	if len(os.Args) >= 3 {
		port = os.Args[2]
	}
	server := internal.NewServer(fpath)
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.NewHTTPHandler(),
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8000,
	}
	err := httpServer.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
