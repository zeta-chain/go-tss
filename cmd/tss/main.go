package main

import (
	"context"
	"flag"
	"fmt"

	golog "github.com/ipfs/go-log"
	"github.com/whyrusleeping/go-logging"

	go_tss "gitlab.com/thorchain/tss/go-tss"
)

func main() {
	golog.SetAllLoggers(logging.INFO)
	_ = golog.SetLogLevel("tss-lib", "DEBUG")
	http := flag.Int("http", 8080, "http port")
	help := flag.Bool("h", false, "Display Help")
	config, err := go_tss.ParseFlags()
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
	tss, err := go_tss.NewTss(config.BootstrapPeers, config.Port, *http)
	if nil != err {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tss.Start(ctx); nil != err {
		panic(err)
	}
}
