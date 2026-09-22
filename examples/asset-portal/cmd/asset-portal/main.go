package main

import (
	"log"

	"example.com/asset-portal/internal/routes"
	"gateway-runtime/core"
)

func main() {
	if err := core.Run(core.ApplicationFunc(routes.Register)); err != nil {
		log.Fatal(err)
	}
}
