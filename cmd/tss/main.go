package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/client/input"
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
		flag.PrintDefaults()
		return
	}
	inBuf := bufio.NewReader(os.Stdin)
	priKeyBytes, err := input.GetPassword("input node secret key:", inBuf)
	if err != nil {
		fmt.Printf("error in get the secret key: %s\n", err.Error())
		return
	}
	tss, err := go_tss.NewTss(config.BootstrapPeers, config.Port, *http, []byte(priKeyBytes))
	if nil != err {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tss.Start(ctx); nil != err {
		panic(err)
	}
}
