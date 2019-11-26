package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/urfave/cli"
	"gitlab.com/thorchain/bepswap/thornode/bifrost/thorclient"
	"gitlab.com/thorchain/bepswap/thornode/cmd"

	gt "gitlab.com/thorchain/tss/go-tss"
)

func main() {
	app := cli.NewApp()
	app.Name = "TSS keygen client"
	app.Usage = "TSS keygen client used to send keygen request to local TSS keygen node"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:     "name",
			Usage:    "name",
			Required: true,
			Hidden:   false,
		},
		cli.StringFlag{
			Name:     "password",
			Usage:    "password",
			Required: true,
			Hidden:   false,
		},
		cli.StringFlag{
			Name:     "url,u",
			Usage:    "url of the tss service",
			EnvVar:   "URL",
			Required: true,
			Hidden:   false,
			Value:    "http://127.0.0.1:8080/keygen",
		},
		cli.StringSliceFlag{
			Name:     "pubkey",
			Usage:    "pubkey",
			Required: true,
			Hidden:   false,
		},
	}
	app.Action = appAction
	err := app.Run(os.Args)

	if err != nil {
		log.Fatal(err)
	}
}

func appAction(c *cli.Context) error {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(cmd.Bech32PrefixAccAddr, cmd.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(cmd.Bech32PrefixValAddr, cmd.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(cmd.Bech32PrefixConsAddr, cmd.Bech32PrefixConsPub)
	config.Seal()
	name := c.String("name")
	password := c.String("password")
	k, err := thorclient.NewKeys("", name, password)
	if nil != err {
		return err
	}
	privateKey, err := k.GetPrivateKey()
	if nil != err {
		return err
	}

	pk, _ := privateKey.(secp256k1.PrivKeySecp256k1)
	hexPrivKey := hex.EncodeToString(pk[:])
	priKey := base64.StdEncoding.EncodeToString([]byte(hexPrivKey))
	pubkeys := c.StringSlice("pubkey")
	//for _, item := range pubkeys {
	//	pk, err := sdk.GetAccPubKeyBech32(item)
	//	if nil != err {
	//		return err
	//	}
	//	addresses = append(addresses, base64.StdEncoding.EncodeToString(pk.Address().Bytes()))
	//	pubkeys = append(pubkeys, base64.StdEncoding.EncodeToString(pk.Bytes()))
	//}
	url := c.String("url")
	req := gt.KeyGenReq{
		PrivKey: priKey,
		Keys:    pubkeys,
	}
	reqBytes, err := json.Marshal(req)
	if nil != err {
		return fmt.Errorf("fail to marshal req to json: %w", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBytes))
	if nil != err {
		return fmt.Errorf("fail to send keygen request to(%s): %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); nil != err {
			fmt.Printf("fail to close response body: %s", err.Error())
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status code from key gen: %d", resp.StatusCode)
	}
	result, err := ioutil.ReadAll(resp.Body)
	if nil != err {
		return fmt.Errorf("fail to read from response body: %w", err)
	}
	fmt.Println(result)
	return nil
}
