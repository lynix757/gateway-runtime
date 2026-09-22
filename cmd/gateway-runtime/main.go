package main

import (
	"log"

	"gateway-runtime/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
