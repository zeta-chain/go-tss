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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/whyrusleeping/go-logging"

	"gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/tss/common"
	"gitlab.com/thorchain/tss/go-tss"
)

func main() {
	golog.SetAllLoggers(logging.INFO)
	_ = golog.SetLogLevel("tss-lib", "DEBUG")
	http := flag.Int("http", 8080, "http port")
	help := flag.Bool("h", false, "Display Help")
	logLevel := flag.String("loglevel", "info", "Log Level")
	pretty := flag.Bool("pretty-log", false, "Enables unstructured prettified logging. This is useful for local debugging")
	baseFolder := flag.String("home", "", "home folder to store the keygen state file")
	config, err := common.ParseFlags()
	if err != nil {
		panic(err)
	}

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
	tss, err := tss.NewTss(config.BootstrapPeers, config.Port, *http, []byte(priKeyBytes), *baseFolder)
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
