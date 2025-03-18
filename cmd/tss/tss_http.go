package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/keysign"
	"github.com/zeta-chain/go-tss/tss"
)

// HTTPServer provide http endpoint for tss server
type HTTPServer struct {
	logger    zerolog.Logger
	tssServer tss.IServer
	s         *http.Server
}

// NewHTTPServer should only listen to the loopback
func NewHTTPServer(tssAddr string, t tss.IServer) *HTTPServer {
	hs := &HTTPServer{
		logger:    log.With().Str("module", "http").Logger(),
		tssServer: t,
	}
	s := &http.Server{
		Addr:    tssAddr,
		Handler: hs.tssNewHandler(),
	}
	hs.s = s
	return hs
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *HTTPServer) tssNewHandler() http.Handler {
	router := mux.NewRouter()
	router.Handle("/keygen", http.HandlerFunc(t.keygenHandler)).Methods(http.MethodPost)
	router.Handle("/keygenall", http.HandlerFunc(t.keygenAllHandler)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keySignHandler)).Methods(http.MethodPost)
	router.Handle("/ping", http.HandlerFunc(t.pingHandler)).Methods(http.MethodGet)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pIDHandler)).Methods(http.MethodGet)
	router.Handle("/metrics", promhttp.Handler())
	router.Use(logMiddleware())
	return router
}

func (t *HTTPServer) keygenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key gen request")
	decoder := json.NewDecoder(r.Body)
	var keygenReq keygen.Request
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp, err := t.tssServer.Keygen(keygenReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to key gen")
	}
	t.logger.Debug().Msgf("resp:%+v", resp)
	buf, err := json.Marshal(resp)
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

func (t *HTTPServer) keygenAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key gen request")
	decoder := json.NewDecoder(r.Body)
	var keygenReq keygen.Request
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp, err := t.tssServer.KeygenAllAlgo(keygenReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to key gen")
	}
	t.logger.Debug().Msgf("resp:%+v", resp)
	buf, err := json.Marshal(resp)
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

func (t *HTTPServer) keySignHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()

	var keySignReq keysign.Request

	if err := json.NewDecoder(r.Body).Decode(&keySignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	signResp, err := t.tssServer.KeySign(keySignReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to key sign")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jsonResult, err := json.MarshalIndent(signResp, "", "  ")
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(jsonResult); err != nil {
		t.logger.Error().Err(err).Msg("fail to write response")
	}
}

func (t *HTTPServer) Start() error {
	if t.s == nil {
		return errors.New("invalid http server instance")
	}
	if err := t.tssServer.Start(); err != nil {
		return fmt.Errorf("fail to start tss server: %w", err)
	}
	if err := t.s.ListenAndServe(); err != nil {
		if err != http.ErrServerClosed {
			return fmt.Errorf("fail to start http server: %w", err)
		}
	}

	return nil
}

func logMiddleware() mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Debug().
				Str("route", r.URL.Path).
				Str("port", r.URL.Port()).
				Str("method", r.Method).
				Msg("HTTP request received")

			handler.ServeHTTP(w, r)
		})
	}
}

func (t *HTTPServer) Stop() error {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := t.s.Shutdown(c)
	if err != nil {
		log.Error().Err(err).Msg("Failed to shutdown the Tss server gracefully")
	}
	t.tssServer.Stop()
	return err
}

func (t *HTTPServer) pingHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (t *HTTPServer) getP2pIDHandler(w http.ResponseWriter, _ *http.Request) {
	localPeerID := t.tssServer.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
