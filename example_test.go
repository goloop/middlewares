package middlewares_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/goloop/middlewares"
)

// ExampleChain composes several middleware into one wrapper.
func ExampleChain() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	})

	h := middlewares.Chain(
		middlewares.RequestID(),
		middlewares.Recoverer(),
		middlewares.SecurityHeaders(),
	)(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hello", nil))
	fmt.Println(rec.Body.String())
	fmt.Println(rec.Header().Get("X-Content-Type-Options"))
	// Output:
	// hello
	// nosniff
}

// ExampleRequestID reuses a fixed generator so the identifier is predictable.
func ExampleRequestID() {
	h := middlewares.RequestID(
		middlewares.WithRequestIDGenerator(func() string { return "req-1" }),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(middlewares.RequestIDFrom(r.Context()))
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Output: req-1
}

// ExampleCORS answers a preflight request from an allowed origin.
func ExampleCORS() {
	h := middlewares.CORS(
		middlewares.WithAllowedOrigins("https://app.example.com"),
		middlewares.WithAllowedMethods("GET", "POST"),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Access-Control-Allow-Origin"))
	// Output:
	// 204
	// https://app.example.com
}
