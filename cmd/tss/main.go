package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	golog "github.com/ipfs/go-log"
	"github.com/whyrusleeping/go-logging"

	"golang.org/x/crypto/ssh/terminal"

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
	priKeyBytes, err := readPassword("input node secret key:")
	if err != nil {
		fmt.Printf("error in get the secret key: %s\n", err.Error())
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
func readPassword(prompt string) (pw []byte, err error) {
	fd := int(os.Stdin.Fd())
	if terminal.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		pw, err = terminal.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return
	}

	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		// terminal.ReadPassword discards any '\r', so we do the same
		if n > 0 && b[0] != '\r' {
			if b[0] == '\n' {
				return pw, nil
			}
			pw = append(pw, b[0])
			// limit size, so that a wrong input won't fill up the memory
			if len(pw) > 1024 {
				err = errors.New("password too long")
			}
		}
		if err != nil {
			// terminal.ReadPassword accepts EOF-terminated passwords
			// if non-empty, so we do the same
			if err == io.EOF && len(pw) > 0 {
				err = nil
			}
			return pw, err
		}
	}
}
