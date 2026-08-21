package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	db "github.com/garinesaiajay/commerceos/infra/db"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable"
	}

	ctx := context.Background()

	dbPool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		fmt.Printf("failed to connect to postgres: %v\n", err)
		os.Exit(1)
	}

	defer dbPool.Close()

	fmt.Println("Connected to PostgreSQL")

	// Catalog
	catalogRepo := catalog.NewPostgresRepository(dbPool)
	catalogService := catalog.NewService(catalogRepo)
	catalogHandler := catalog.NewHandler(catalogService)

	// Cart
	cartRepo := cart.NewPostgresRepository(dbPool)
	cartService := cart.NewService(cartRepo, catalogRepo)
	cartHandler := cart.NewHandler(cartService)

	// Service routers
	apiGatewayMux := http.NewServeMux()
	commerceMux := http.NewServeMux()
	agentAPIMux := http.NewServeMux()
	dashboardMux := http.NewServeMux()

	// API Gateway
	apiGatewayMux.HandleFunc("/health", healthHandler)

	// Commerce Service
	commerceMux.HandleFunc("/health", healthHandler)

	commerceMux.HandleFunc(
		"/products",
		catalogHandler.ListProducts,
	)

	commerceMux.HandleFunc(
		"/products/",
		catalogHandler.GetProduct,
	)

	commerceMux.HandleFunc("/variants/", catalogHandler.GetVariant)

	commerceMux.HandleFunc(
		"/carts",
		cartHandler.CreateCart,
	)

	commerceMux.HandleFunc(
		"/carts/",
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/items"):
				cartHandler.AddItem(w, r)

			case r.Method == http.MethodPatch &&
				strings.Contains(r.URL.Path, "/items/"):
				cartHandler.UpdateItemQuantity(w, r)

			case r.Method == http.MethodDelete &&
				strings.Contains(r.URL.Path, "/items/"):
				cartHandler.RemoveItem(w, r)

			case r.Method == http.MethodGet:
				cartHandler.GetCart(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	)

	// Agent API Service
	agentAPIMux.HandleFunc("/health", healthHandler)

	// Dashboard API
	dashboardMux.HandleFunc("/health", healthHandler)

	// API Gateway
	go func() {
		fmt.Println("API Gateway listening on :8080")
		http.ListenAndServe(":8080", apiGatewayMux)
	}()

	// Commerce Service
	go func() {
		fmt.Println("Commerce Service listening on :8081")
		http.ListenAndServe(":8081", commerceMux)
	}()

	// Agent API Service
	go func() {
		fmt.Println("Agent API Service listening on :8082")
		http.ListenAndServe(":8082", agentAPIMux)
	}()

	// Dashboard API
	go func() {
		fmt.Println("Dashboard API listening on :8083")
		http.ListenAndServe(":8083", dashboardMux)
	}()

	select {}
}
