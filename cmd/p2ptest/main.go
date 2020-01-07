package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	golog "github.com/ipfs/go-log"
	"github.com/rs/zerolog/log"
	"github.com/whyrusleeping/go-logging"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func main() {
	golog.SetAllLoggers(logging.INFO)
	if err := golog.SetLogLevel("tss_p2p", "DEBUG"); nil != err {
		panic(err)
	}
	help := flag.Bool("h", false, "Display Help")
	config, err := common.ParseFlags()
	if err != nil {
		panic(err)
	}

	if *help {
		fmt.Println("This program demonstrates a simple p2p chat application using libp2p")
		fmt.Println()
		fmt.Println("Usage: Run './chat in two different terminals. Let them connect to the bootstrap nodes, announce themselves and connect to the peers")
		flag.PrintDefaults()
		return
	}
	c, err := p2p.NewCommunication("tss", config.BootstrapPeers, config.Port)
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
