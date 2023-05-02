package main

import (
	"net/http"

	"github.com/ejuju/boltdb_webgui/app"
)

func main() {
	h := app.NewHTTPHandler("test.boltdb")
	err := http.ListenAndServe(":8081", h)
	if err != nil {
		panic(err)
	}
}
