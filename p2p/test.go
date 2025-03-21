package p2p

import (
	"net"

	"github.com/pkg/errors"
)

// GetFreePorts allocated N available ports for testing purposes
// See https://github.com/tendermint/tendermint/issues/3682
func GetFreePorts(n int) ([]int, error) {
	ports := make([]int, 0, n)

	for i := 0; i < n; i++ {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return nil, errors.Wrap(err, "unable to resolve port")
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, errors.Wrap(err, "unable to listen to tcp")
		}

		// This is done on purpose - we want to keep ports
		// busy to avoid collisions when getting the next one
		//goland:noinspection ALL
		defer func() { _ = l.Close() }()

		port := l.Addr().(*net.TCPAddr).Port
		ports = append(ports, port)
	}

	return ports, nil
}
