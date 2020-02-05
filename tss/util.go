package tss

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

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

func StopServer(server *http.Server) error {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := server.Shutdown(c)
	if err != nil {
		log.Error().Err(err).Msg("Failed to shutdown the Tss server gracefully")
	}
	return err
}

func getPriKey(priKeyString string) (cryptokey.PrivKey, error) {
	priHexBytes, err := base64.StdEncoding.DecodeString(priKeyString)
	if err != nil {
		return nil, fmt.Errorf("fail to decode private key: %w", err)
	}
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if err != nil {
		return nil, fmt.Errorf("fail to hex decode private key: %w", err)
	}
	var keyBytesArray [32]byte
	copy(keyBytesArray[:], rawBytes[:32])
	priKey := secp256k1.PrivKeySecp256k1(keyBytesArray)
	return priKey, nil
}

func getPriKeyRawBytes(priKey cryptokey.PrivKey) ([]byte, error) {
	var keyBytesArray [32]byte
	pk, ok := priKey.(secp256k1.PrivKeySecp256k1)
	if !ok {
		return nil, errors.New("private key is not secp256p1.PrivKeySecp256k1")
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}
