package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/binance-chain/go-sdk/common/types"
	"github.com/cosmos/cosmos-sdk/client/input"
	golog "github.com/ipfs/go-log"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/tss"
)

func main() {
	// Parse the cli into configuration structs
	generalConf, tssConf, p2pConf := parseFlags()
	if generalConf.Help {
		flag.PrintDefaults()
		return
	}
	// Setup logging
	golog.SetAllLoggers(golog.LevelInfo)
	_ = golog.SetLogLevel("tss-lib", "INFO")
	common.InitLog(generalConf.LogLevel, generalConf.Pretty, "tss_service")

	// Setup Bech32 Prefixes
	common.SetupBech32Prefix()
	// this is only need for the binance library
	if os.Getenv("NET") == "testnet" || os.Getenv("NET") == "mocknet" {
		types.Network = types.TestNetwork
	}
	// Read stdin for the private key
	inBuf := bufio.NewReader(os.Stdin)
	priKeyBytes, err := input.GetPassword("input node secret key:", inBuf)
	if err != nil {
		fmt.Printf("error in get the secret key: %s\n", err.Error())
		return
	}
	priKey, err := tss.GetPriKey(priKeyBytes)
	if err != nil {
		log.Fatal(err)
	}
	// init tss module
	tss, err := tss.NewTss(
		p2pConf.BootstrapPeers,
		p2pConf.Port,
		priKey,
		p2pConf.RendezvousString,
		generalConf.BaseFolder,
		tssConf,
		nil,
	)
	if nil != err {
		log.Fatal(err)
	}
	s := NewTssHttpServer(generalConf.TssAddr, tss)
	if err := s.Start(); err != nil {
		log.Fatal(err)
	}
}

// parseFlags - Parses the cli flags
func parseFlags() (generalConf common.GeneralConfig, tssConf common.TssConfig, p2pConf p2p.P2PConfig) {
	// we setup the configure for the general configuration
	flag.StringVar(&generalConf.TssAddr, "tss-port", "127.0.0.1:8080", "tss port")
	flag.BoolVar(&generalConf.Help, "h", false, "Display Help")
	flag.StringVar(&generalConf.LogLevel, "loglevel", "info", "Log Level")
	flag.BoolVar(&generalConf.Pretty, "pretty-log", false, "Enables unstructured prettified logging. This is useful for local debugging")
	flag.StringVar(&generalConf.BaseFolder, "home", "", "home folder to store the keygen state file")

	// we setup the Tss parameter configuration
	flag.DurationVar(&tssConf.KeyGenTimeout, "gentimeout", 30*time.Second, "keygen timeout")
	flag.DurationVar(&tssConf.KeySignTimeout, "signtimeout", 30*time.Second, "keysign timeout")
	flag.DurationVar(&tssConf.PreParamTimeout, "preparamtimeout", 5*time.Minute, "pre-parameter generation timeout")

	// we setup the p2p network configuration
	flag.StringVar(&p2pConf.RendezvousString, "rendezvous", "Asgard",
		"Unique string to identify group of nodes. Share this with your friends to let them connect with you")
	flag.IntVar(&p2pConf.Port, "p2p-port", 6668, "listening port local")
	flag.Var(&p2pConf.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Parse()
	return
}
