package common

import (
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"
)

type EncryptionSuite struct{}

var _ = Suite(&EncryptionSuite{})

func (s *EncryptionSuite) TestEncryption(c *C) {
	body := []byte("hello world!")
	sk := secp256k1.GenPrivKey()
	h := sha3.New256()
	h.Write(sk.Bytes())
	passcode := h.Sum(nil)
	encrypted, err := AESEncrypt(body, passcode)
	c.Assert(err, IsNil)
	decrypted, err := AESDecrypt(encrypted, passcode)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, decrypted)

	encrypted, err = AESEncrypt(nil, nil)
	c.Assert(err, NotNil)
	decrypted, err = AESDecrypt(nil, passcode)
	c.Assert(err, NotNil)
}
