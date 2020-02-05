package tss

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func NewInfoHttpServer(infoAddr string, t *TssServer) *http.Server {
	server := &http.Server{
		Addr:         infoAddr,
		Handler:      t.infoHandler(true),
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return server
}

func (t *TssServer) infoHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", http.HandlerFunc(t.ping)).Methods(http.MethodGet)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pID)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *TssServer) ping(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (t *TssServer) getP2pID(w http.ResponseWriter, _ *http.Request) {
	localPeerID := t.p2pCommunication.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
