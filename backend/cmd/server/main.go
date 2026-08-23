package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/garinesaiajay/commerceos/commerce/cart"
	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/commerce/payment"
	db "github.com/garinesaiajay/commerceos/infra/db"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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

	// -------------------------
	// Catalog
	// -------------------------

	catalogRepo := catalog.NewPostgresRepository(dbPool)
	catalogService := catalog.NewService(catalogRepo)
	catalogHandler := catalog.NewHandler(catalogService)

	// -------------------------
	// Cart
	// -------------------------

	cartRepo := cart.NewPostgresRepository(dbPool)
	cartService := cart.NewService(cartRepo, catalogRepo)
	cartHandler := cart.NewHandler(cartService)

	// -------------------------
	// Order
	// -------------------------

	orderRepo := order.NewPostgresRepository(dbPool)
	orderService := order.NewService(orderRepo)
	orderHandler := order.NewHandler(orderService)

	// -------------------------
	// Payment
	// -------------------------

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")

	if razorpayKeyID == "" || razorpayKeySecret == "" {
		fmt.Println("RAZORPAY_KEY_ID and RAZORPAY_KEY_SECRET must be set")
		os.Exit(1)
	}

	razorpayClient := payment.NewRazorpayClient(
		razorpayKeyID,
		razorpayKeySecret,
	)

	paymentRepo := payment.NewPostgresRepository(dbPool)
	paymentAttemptRepo := payment.NewPostgresAttemptRepository(dbPool)

	paymentService := payment.NewServiceWithAttempts(
		razorpayClient,
		paymentRepo,
		paymentAttemptRepo,
	)

	paymentHandler := payment.NewHandler(
		paymentService,
		orderRepo,
	)

	webhookHandler := payment.NewWebhookHandler()

	// -------------------------
	// Service routers
	// -------------------------

	apiGatewayMux := http.NewServeMux()
	commerceMux := http.NewServeMux()
	agentAPIMux := http.NewServeMux()
	dashboardMux := http.NewServeMux()

	// -------------------------
	// API Gateway
	// -------------------------

	apiGatewayMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Commerce Service
	// -------------------------

	commerceMux.HandleFunc("/health", healthHandler)

	// Catalog
	commerceMux.HandleFunc(
		"/products",
		catalogHandler.ListProducts,
	)

	commerceMux.HandleFunc(
		"/products/",
		catalogHandler.GetProduct,
	)

	commerceMux.HandleFunc(
		"/variants/",
		catalogHandler.GetVariant,
	)

	// Cart
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

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/checkout"):
				orderHandler.Checkout(w, r)

			case r.Method == http.MethodGet:
				cartHandler.GetCart(w, r)

			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		},
	)

	commerceMux.HandleFunc(
		"/orders/",
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment/verify"):
				paymentHandler.VerifyPayment(w, r)

			case r.Method == http.MethodPost &&
				strings.HasSuffix(r.URL.Path, "/payment"):
				paymentHandler.CreatePaymentOrder(w, r)

			default:
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
			}
		},
	)

	commerceMux.HandleFunc(
		"/webhooks/razorpay",
		webhookHandler.HandleRazorpay,
	)

	// -------------------------
	// Agent API Service
	// -------------------------

	agentAPIMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Dashboard API
	// -------------------------

	dashboardMux.HandleFunc("/health", healthHandler)

	// -------------------------
	// Start API Gateway
	// -------------------------

	apiGatewayPort := os.Getenv("API_GATEWAY_PORT")
	if apiGatewayPort == "" {
		apiGatewayPort = "8080"
	}

	go func() {
		fmt.Printf("API Gateway listening on :%s\n", apiGatewayPort)

		if err := http.ListenAndServe(
			":"+apiGatewayPort,
			apiGatewayMux,
		); err != nil {
			fmt.Printf("API Gateway stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Commerce Service
	// -------------------------

	commercePort := os.Getenv("COMMERCE_PORT")
	if commercePort == "" {
		commercePort = "8081"
	}

	go func() {
		fmt.Printf("Commerce Service listening on :%s\n", commercePort)

		if err := http.ListenAndServe(
			":"+commercePort,
			corsMiddleware(commerceMux),
		); err != nil {
			fmt.Printf("Commerce Service stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Agent API Service
	// -------------------------

	agentAPIPort := os.Getenv("AGENT_API_PORT")
	if agentAPIPort == "" {
		agentAPIPort = "8082"
	}

	go func() {
		fmt.Printf("Agent API Service listening on :%s\n", agentAPIPort)

		if err := http.ListenAndServe(
			":"+agentAPIPort,
			agentAPIMux,
		); err != nil {
			fmt.Printf("Agent API Service stopped: %v\n", err)
		}
	}()

	// -------------------------
	// Start Dashboard API
	// -------------------------

	dashboardPort := os.Getenv("DASHBOARD_PORT")
	if dashboardPort == "" {
		dashboardPort = "8083"
	}

	go func() {
		fmt.Printf("Dashboard API listening on :%s\n", dashboardPort)

		if err := http.ListenAndServe(
			":"+dashboardPort,
			dashboardMux,
		); err != nil {
			fmt.Printf("Dashboard Service stopped: %v\n", err)
		}
	}()

	select {}
}
