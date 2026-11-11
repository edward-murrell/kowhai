package api

import (
	"net/http"

	apispec "kowhai/api"
	"kowhai/internal/service"
)

// Mount registers the REST API, its event streams and its docs on mux.
func Mount(mux *http.ServeMux, s *service.Service, st *Streams) {
	strict := NewStrictHandlerWithOptions(&Handlers{S: s}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestError,
		ResponseErrorHandlerFunc: WriteError,
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux, ErrorHandlerFunc: requestError})

	mux.HandleFunc("GET /api/v1/boards/{id}/events", st.BoardEvents)
	mux.HandleFunc("GET /api/v1/me/events", st.UserEvents)
	mux.HandleFunc("POST /api/v1/cards/{id}/attachments", st.Upload)
	mux.HandleFunc("GET /api/v1/attachments/{id}/download", st.Download)

	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(apispec.YAML)
	})
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, http.StatusNotFound, "Not found", "no such endpoint")
	})
}

const docsPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Kōwhai API</title>
</head>
<body>
<script id="api-reference" data-url="/api/v1/openapi.yaml"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.25.130"></script>
</body>
</html>
`
