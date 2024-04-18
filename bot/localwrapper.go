package main

import (
	"fmt"
	"github.com/bugfloyd/anonymous-telegram-bot/anonymous"
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Read the body of the incoming HTTP request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request", http.StatusInternalServerError)
			return
		}

		// Create a Request object that mimics API Gateway
		req := anonymous.APIRequest{
			Body: string(body),
		}

		// Invoke the Lambda handler from your main package
		resp, err := anonymous.InitBot(req)
		if err != nil {
			log.Printf("Error handling request: %v", err)
			http.Error(w, fmt.Sprintf("Handler error: %v", err), http.StatusInternalServerError)
			return
		}

		// Write the Lambda response back as an HTTP response
		for key, value := range resp.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write([]byte(resp.Body))
	})

	// Start the HTTP server on port 8080
	log.Println("Starting HTTP server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
