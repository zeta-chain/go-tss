package tss

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func NewInfoHttpServer(infoAddr string, t *TssServer) *http.Server {
	server := &http.Server{
		Addr:         infoAddr,
		Handler:      t.infoHandler(),
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return server
}

func (t *TssServer) infoHandler() http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", http.HandlerFunc(t.pingHandler)).Methods(http.MethodGet)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pIDHandler)).Methods(http.MethodGet)
	router.Use(logMiddleware())
	return router
}

func (t *TssServer) pingHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (t *TssServer) getP2pIDHandler(w http.ResponseWriter, _ *http.Request) {
	localPeerID := t.p2pCommunication.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
