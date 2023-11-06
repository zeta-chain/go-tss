package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tnet "github.com/libp2p/go-libp2p-testing/net"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/tss-lib/ecdsa/keygen"
	. "gopkg.in/check.v1"
)

type FileStateMgrTestSuite struct{}

var _ = Suite(&FileStateMgrTestSuite{})

func TestPackage(t *testing.T) { TestingT(t) }

func (s *FileStateMgrTestSuite) SetUpTest(c *C) {
	_ = os.Setenv(keyFragmentSeed, "vSeb9Pw5GEAcZHVX1AgxPPWeyPLWTfWkGWNXeDKFO2zHoqDsRnVVoLE4fEIcO14d")
	conversion.SetupBech32Prefix()
}

func (s *FileStateMgrTestSuite) TestNewFileStateMgr(c *C) {
	folder := os.TempDir()
	f := filepath.Join(folder, "test", "test1", "test2")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	fileName, err := fsm.getFilePathName("whatever")
	c.Assert(err, NotNil)
	fileName, err = fsm.getFilePathName("thorpub1addwnpepqf90u7n3nr2jwsw4t2gzhzqfdlply8dlzv3mdj4dr22uvhe04azq5gac3gq")
	c.Assert(err, IsNil)
	c.Assert(fileName, Equals, filepath.Join(f, "localstate-thorpub1addwnpepqf90u7n3nr2jwsw4t2gzhzqfdlply8dlzv3mdj4dr22uvhe04azq5gac3gq.json"))
}

func (s *FileStateMgrTestSuite) TestSaveLocalState(c *C) {
	stateItem := KeygenLocalState{
		PubKey:    "wasdfasdfasdfasdfasdfasdf",
		LocalData: keygen.NewLocalPartySaveData(5),
		ParticipantKeys: []string{
			"A", "B", "C",
		},
		LocalPartyKey: "A",
	}
	folder := os.TempDir()
	f := filepath.Join(folder, "test", "test1", "test2")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	c.Assert(fsm.SaveLocalState(stateItem), NotNil)
	stateItem.PubKey = "thorpub1addwnpepqf90u7n3nr2jwsw4t2gzhzqfdlply8dlzv3mdj4dr22uvhe04azq5gac3gq"
	c.Assert(fsm.SaveLocalState(stateItem), IsNil)
	filePathName := filepath.Join(f, "localstate-"+stateItem.PubKey+".json")
	_, err = os.Stat(filePathName)
	c.Assert(err, IsNil)
	item, err := fsm.GetLocalState(stateItem.PubKey)
	c.Assert(err, IsNil)
	c.Assert(reflect.DeepEqual(stateItem, item), Equals, true)
}

func (s *FileStateMgrTestSuite) TestSaveAddressBook(c *C) {
	testAddresses := make(map[peer.ID]p2p.AddrList)
	var t *testing.T
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	mockAddr, err := maddr.NewMultiaddr("/ip4/192.168.3.5/tcp/6668")
	c.Assert(err, IsNil)
	peers := []peer.ID{id1.ID(), id2.ID(), id3.ID()}
	for _, each := range peers {
		testAddresses[each] = []p2p.Multiaddr{mockAddr}
	}
	folder := os.TempDir()
	f := filepath.Join(folder, "test")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	c.Assert(fsm.SaveAddressBook(testAddresses), IsNil)
	filePathName := filepath.Join(f, "address_book.seed")
	_, err = os.Stat(filePathName)
	c.Assert(err, IsNil)
	item, err := fsm.RetrieveP2PAddresses()
	c.Assert(err, IsNil)
	c.Assert(item, HasLen, 3)
}

func (s *FileStateMgrTestSuite) TestEncryption(c *C) {
	folder := os.TempDir()
	f := filepath.Join(folder, "test", "test1", "test2")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)

	stateItem := KeygenLocalState{
		PubKey:    "wasdfasdfasdfasdfasdfasdf",
		LocalData: keygen.NewLocalPartySaveData(5),
		ParticipantKeys: []string{
			"A", "B", "C",
		},
		LocalPartyKey: "A",
	}
	buf, err := json.Marshal(stateItem)
	c.Assert(buf, NotNil)

	ct, err := fsm.encryptFragment(buf)
	c.Assert(err, IsNil)
	pt, err := fsm.decryptFragment(ct)
	c.Assert(err, IsNil)
	c.Assert(reflect.DeepEqual(buf, pt), Equals, true)
}
