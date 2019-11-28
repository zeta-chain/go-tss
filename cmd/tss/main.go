package main

import (
	"context"
	"flag"
	"fmt"

	golog "github.com/ipfs/go-log"
	"github.com/whyrusleeping/go-logging"

	go_tss "gitlab.com/thorchain/tss/go-tss"
	"golang.org/x/crypto/ssh/terminal"
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
	fmt.Println("input node secret key:")
	priKeyBytes, err := terminal.ReadPassword(0)
	if err != nil {
		fmt.Println("error in get the secret key")
		return
	}
	tss, err := go_tss.NewTss(config.BootstrapPeers, config.Port, *http, priKeyBytes)
	if nil != err {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tss.Start(ctx, priKeyBytes); nil != err {
		panic(err)
	}
}
