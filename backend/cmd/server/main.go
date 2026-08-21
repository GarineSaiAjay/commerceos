package main

import (
	"fmt"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func main() {
	apiGatewayMux := http.NewServeMux()
	commerceMux := http.NewServeMux()
	agentAPIMux := http.NewServeMux()
	dashboardMux := http.NewServeMux()

	apiGatewayMux.HandleFunc("/health", healthHandler)
	commerceMux.HandleFunc("/health", healthHandler)
	agentAPIMux.HandleFunc("/health", healthHandler)
	dashboardMux.HandleFunc("/health", healthHandler)

	go func() {
		fmt.Println("API Gateway listening on :8080")
		http.ListenAndServe(":8080", apiGatewayMux)
	}()

	go func() {
		fmt.Println("Commerce Service listening on :8081")
		http.ListenAndServe(":8081", commerceMux)
	}()

	go func() {
		fmt.Println("Agent API Service listening on :8082")
		http.ListenAndServe(":8082", agentAPIMux)
	}()

	go func() {
		fmt.Println("Dashboard API listening on :8083")
		http.ListenAndServe(":8083", dashboardMux)
	}()

	select {}
}
