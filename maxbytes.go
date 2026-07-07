package middlewares

import "net/http"

// MaxBytes limits the size of a request body to n bytes using
// http.MaxBytesReader. A handler that reads past the limit gets an error, and
// the server responds appropriately. Use it to guard against oversized uploads.
func MaxBytes(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
