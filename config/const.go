// Package config contains the constants for the TSS library
package config

import "time"

// stream-related constants
const (
	StreamTimeoutConnect = 20 * time.Second
	StreamTimeoutRead    = 20 * time.Second
	StreamTimeoutWrite   = 20 * time.Second
	StreamMaxPayload     = 1024 * 1024 * 10 // 20MB
)
