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
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func main() {
	p2pConf := p2p.P2PConfig{}
	tssConf := common.TssConfig{}
	generalConf := common.GeneralConfig{}
	golog.SetAllLoggers(logging.INFO)
	_ = golog.SetLogLevel("tss-lib", "INFO")

	parseFlags(&generalConf, &tssConf, &p2pConf)
	if generalConf.Help {
		flag.PrintDefaults()
		return
	}
	initLog(generalConf.LogLevel, generalConf.Pretty)
	inBuf := bufio.NewReader(os.Stdin)
	priKeyBytes, err := input.GetPassword("input node secret key:", inBuf)
	if err != nil {
		fmt.Printf("error in get the secret key: %s\n", err.Error())
		return
	}
	if p2pConf.ProtocolID == "" {
		fmt.Printf("invalid prtocol ID")
		return
	}
	protocolID := protocol.ConvertFromStrings([]string{p2pConf.ProtocolID})[0]
	tss, err := tss.NewTss(p2pConf.BootstrapPeers, p2pConf.Port, generalConf.TssPort, generalConf.ExternalPort, protocolID, []byte(priKeyBytes), p2pConf.RendezvousString, generalConf.BaseFolder, tssConf)
	if nil != err {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tss.Start(ctx); nil != err {
		panic(err)
	}
}

func parseFlags(generalConf *common.GeneralConfig, tssConf *common.TssConfig, p2pConf *p2p.P2PConfig) {
	//we setup the configure for the general configuration
	flag.IntVar(&generalConf.TssPort, "tss-port", 8080, "http port")
	flag.IntVar(&generalConf.ExternalPort, "external-port", 8081, "http port")
	flag.BoolVar(&generalConf.Help, "h", false, "Display Help")
	flag.StringVar(&generalConf.LogLevel, "loglevel", "info", "Log Level")
	flag.BoolVar(&generalConf.Pretty, "pretty-log", false, "Enables unstructured prettified logging. This is useful for local debugging")
	flag.StringVar(&generalConf.BaseFolder, "home", "", "home folder to store the keygen state file")

	//we setup the Tss parameter configuration
	flag.DurationVar(&tssConf.KeyGenTimeout, "gentimeout", 30, "keygen timeout (second)")
	flag.DurationVar(&tssConf.KeySignTimeout, "signtimeout", 30, "keysign timeout (second)")
	flag.DurationVar(&tssConf.SyncTimeout, "synctimeout", 5, "node sync wait time (second)")
	flag.DurationVar(&tssConf.PreParamTimeout, "preparamtimeout", 5, "pre-parameter generation timeout")
	flag.IntVar(&tssConf.SyncRetry, "syncretry", 20, "retry of node sync")

	//we setup the p2p network configuration
	flag.StringVar(&p2pConf.RendezvousString, "rendezvous", "Asgard",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.StringVar(&p2pConf.ProtocolID, "protocolID", "tss", "protocol ID for p2p communication")
	flag.IntVar(&p2pConf.Port, "p2p-port", 6668, "listening port local")
	flag.Var(&p2pConf.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Parse()
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
