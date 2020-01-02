package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cosmos/cosmos-sdk/client/input"
	golog "github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/whyrusleeping/go-logging"

	"gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/common"
)

func main() {
	config := common.P2PConfig{}
	golog.SetAllLoggers(logging.INFO)
	_ = golog.SetLogLevel("tss-lib", "INFO")

	//we setup the configure for the general configuration
	http := flag.Int("http", 8080, "http port")
	help := flag.Bool("h", false, "Display Help")
	logLevel := flag.String("loglevel", "info", "Log Level")
	pretty := flag.Bool("pretty-log", false, "Enables unstructured prettified logging. This is useful for local debugging")
	baseFolder := flag.String("home", "", "home folder to store the keygen state file")

	//we setup the Tss parameter configuration
	flag.DurationVar(&common.KeyGenTimeout, "gentimeout", 30, "keygen timeout (second)")
	flag.DurationVar(&common.KeySignTimeout, "signtimeout", 30, "keysign timeout (second)")
	flag.DurationVar(&common.SyncTimeout, "synctimeout", 5, "node sync wait time (second)")
	flag.DurationVar(&common.PreParamTimeout, "preparamtimeout", 5, "pre-parameter generation timeout")
	flag.IntVar(&common.SyncRetry, "syncretry", 20, "retry of node sync")

	//we setup the p2p network configuration
	flag.StringVar(&config.RendezvousString, "rendezvous", "Asgard",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.StringVar(&config.ProtocolID, "protocolID", "tss", "protocol ID for p2p communication")
	flag.IntVar(&config.Port, "port", 6668, "listening port local")
	flag.Var(&config.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}
	initLog(*logLevel, *pretty)
	inBuf := bufio.NewReader(os.Stdin)
	priKeyBytes, err := input.GetPassword("input node secret key:", inBuf)
	if err != nil {
		fmt.Printf("error in get the secret key: %s\n", err.Error())
		return
	}
	if config.ProtocolID == "" {
		fmt.Printf("invalid prtocol ID")
		return
	}
	protocolID := protocol.ConvertFromStrings([]string{config.ProtocolID})[0]
	tss, err := tss.NewTss(config.BootstrapPeers, config.Port, *http, protocolID, []byte(priKeyBytes), config.RendezvousString, *baseFolder)
	if nil != err {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tss.Start(ctx); nil != err {
		panic(err)
	}
}

func initLog(level string, pretty bool) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("%s is not a valid log-level, falling back to 'info'", level)
	}
	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	zerolog.SetGlobalLevel(l)
	log.Logger = log.Output(out).With().Str("service", "go-tss").Logger()
}
