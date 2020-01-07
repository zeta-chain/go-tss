package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	golog "github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog/log"
	"github.com/whyrusleeping/go-logging"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func main() {
	p2pConf := p2p.P2PConfig{}
	generalConf := common.GeneralConfig{}
	golog.SetAllLoggers(logging.INFO)
	if err := golog.SetLogLevel("tss_p2p", "DEBUG"); nil != err {
		panic(err)
	}

	parseFlags(&generalConf, &p2pConf)
	if generalConf.Help {
		fmt.Println("This program demonstrates a simple p2p chat application using libp2p")
		fmt.Println()
		fmt.Println("Usage: Run './chat in two different terminals. Let them connect to the bootstrap nodes, announce themselves and connect to the peers")
		flag.PrintDefaults()
		return
	}
	if p2pConf.ProtocolID == "" {
		panic("error in process protocol ID")
	}
	protocolID := protocol.ConvertFromStrings([]string{p2pConf.ProtocolID})[0]
	c, err := p2p.NewCommunication("tss", p2pConf.BootstrapPeers, p2pConf.Port, protocolID)
	if nil != err {
		panic(err)
	}
	testPriKey := "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="
	if err := c.Start([]byte(testPriKey)); nil != err {
		panic(err)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	if err := c.Stop(); nil != err {
		log.Fatal().Err(err).Msg("fail to stop tss")
	}
}

func parseFlags(generalConf *common.GeneralConfig, p2pConf *p2p.P2PConfig) {

	flag.BoolVar(&generalConf.Help, "h", false, "Display Help")
	flag.StringVar(&p2pConf.RendezvousString, "rendezvous", "Asgard",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.StringVar(&p2pConf.ProtocolID, "protocolID", "tss", "protocol ID for p2p communication")
	flag.Var(&p2pConf.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.IntVar(&p2pConf.Port, "p2p-port", 6668, "listening port local")
	flag.Parse()
}
