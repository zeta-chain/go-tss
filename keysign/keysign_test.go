package keysign

import (
	"io"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	initLog("info", true)
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
	log.Logger = log.Output(out).With().Str("service", "go-tss-test").Logger()
}

func (t *TssTestSuite) TestSignMessage(c *C) {
	req := KeySignReq{
		PoolPubKey: "helloworld",
		Message:    "whatever",
	}
	conf := common.TssConfig{}
	sk, err := common.GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	keySignInstance := NewTssKeySign("", "", conf, sk, nil, nil)
	signatureData, err := keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
	signatureData, err = keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
}
