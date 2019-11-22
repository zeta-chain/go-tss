package go_tss

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TSS
type Tss struct {
	comm   *Communication
	logger zerolog.Logger
	port   int
	server *http.Server
	wg     sync.WaitGroup
}

// NewTss create a new instance of Tss
func NewTss(bootstrapPeers []maddr.Multiaddr, p2pPort, tssPort int) (*Tss, error) {
	if p2pPort == tssPort {
		return nil, errors.New("tss and p2p can't use the same port")
	}
	c, err := NewCommunication(DefaultRendezvous, bootstrapPeers, p2pPort)
	if nil != err {
		return nil, fmt.Errorf("fail to create communication layer: %w", err)
	}

	t := &Tss{
		comm:   c,
		logger: zerolog.Logger{},
		port:   tssPort,
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", tssPort),
		Handler: t.newHandler(true),
	}
	t.server = server
	return t, nil
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *Tss) newHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", ping()).Methods(http.MethodGet)
	router.Handle("/keygen", http.HandlerFunc(t.keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keysign)).Methods(http.MethodPost)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *Tss) keygen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

}

func (t *Tss) keysign(w http.ResponseWriter, r *http.Request) {

}

func ping() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
func logMiddleware(verbose bool) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verbose {
				log.Debug().
					Str("route", r.URL.Path).
					Str("port", r.URL.Port()).
					Str("method", r.Method).
					Msg("HTTP request received")
			}
			handler.ServeHTTP(w, r)
		})
	}
}

// Start Tss server
func (t *Tss) Start(ctx context.Context) error {
	log.Info().Int("port", t.port).Msg("Starting the HTTP server")
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := t.server.Shutdown(c)
		if err != nil {
			log.Error().Err(err).Int("port", t.port).Msg("Failed to shutdown the HTTP server gracefully")
		}
	}()

	if err := t.comm.Start(); nil != err {
		return fmt.Errorf("fail to start p2p communication layer: %w", err)
	}
	err := t.server.ListenAndServe()
	t.wg.Wait()
	if err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Int("port", t.port).Msg("Failed to start the HTTP server")
		return err
	}
	log.Info().Int("port", t.port).Msg("The HTTP server has been stopped successfully")
	return nil
}
