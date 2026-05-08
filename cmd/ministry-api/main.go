package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/vpngen/ministry/internal/pgsql"

	_ "github.com/vpngen/ministry/cmd/ministry-api/docs" // generated swagger docs
)

// @title           Ministry API
// @version         1.0
// @description     Ministry REST API — proxy layer over VIP server (vip.vpn.works).
// @basePath        /
// @schemes         http https

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse config: %s\n", err)
		os.Exit(1)
	}

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /reserve", reserveHandler(db, cfg))
	mux.HandleFunc("/api/documentation/", httpSwagger.WrapHandler)

	log.Printf("Listening on %s\n", cfg.listenAddr)

	if err := http.ListenAndServe(cfg.listenAddr, mux); err != nil {
		log.Fatalf("ListenAndServe: %s\n", err)
	}
}
