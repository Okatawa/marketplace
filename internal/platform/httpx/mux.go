package httpx

import "net/http"

// JSONMux preserves ServeMux routing, redirects and Allow headers, while
// formatting its generated 404/405 responses with the public API contract.
func JSONMux(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern == "" {
			h.ServeHTTP(&routeErrorWriter{ResponseWriter: w, request: r}, r)
			return
		}
		// ServeHTTP populates path values for matched handlers.
		mux.ServeHTTP(w, r)
	})
}

type routeErrorWriter struct {
	http.ResponseWriter
	request *http.Request
	handled bool
}

func (w *routeErrorWriter) WriteHeader(status int) {
	switch status {
	case http.StatusNotFound:
		WriteError(w.ResponseWriter, w.request, Fail(status, "not_found", "Не найдено"))
		w.handled = true
	case http.StatusMethodNotAllowed:
		WriteError(w.ResponseWriter, w.request, Fail(status, "method_not_allowed", "Метод не поддерживается"))
		w.handled = true
	default:
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *routeErrorWriter) Write(b []byte) (int, error) {
	if w.handled {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
