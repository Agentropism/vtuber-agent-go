package main

import (
	"log"

	"onebot-gateway/internal/app"
)

func main() {
	server, err := app.Initialize()
	if err != nil {
		log.Fatal(err)
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
