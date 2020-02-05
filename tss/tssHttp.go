package tss

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// Tssport should only listen to the loopback
func NewTssHttpServer(tssAddr string, t *TssServer) *http.Server {
	server := &http.Server{
		Addr:    tssAddr,
		Handler: t.tssNewHandler(true),
	}
	return server
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *TssServer) tssNewHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/keygen", http.HandlerFunc(t.Keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.KeySign)).Methods(http.MethodPost)
	router.Handle("/nodestatus", http.HandlerFunc(t.getNodeStatus)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *TssServer) getNodeStatus(w http.ResponseWriter, _ *http.Request) {
	buf, err := json.Marshal(t.Status)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(buf)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
