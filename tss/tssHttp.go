package tss

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
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
	router.Handle("/keygen", http.HandlerFunc(t.KeygenHandler)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.KeySignHandler)).Methods(http.MethodPost)
	router.Handle("/nodestatus", http.HandlerFunc(t.getNodeStatusHandler)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *TssServer) KeygenHandler(w http.ResponseWriter, r *http.Request) {
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
	var keygenReq keygen.KeyGenReq
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp, err := t.Keygen(keygenReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to key sign")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

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

func (t *TssServer) KeySignHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key sign request")

	var keySignReq keysign.KeySignReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&keySignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	signResp, err := t.KeySign(keySignReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to key sign")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write response")
	}
}

func (t *TssServer) getNodeStatusHandler(w http.ResponseWriter, _ *http.Request) {
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
