package net

import (
	"github.com/hashicorp/yamux"
	"github.com/ipfs/go-log"
	"github.com/spf13/viper"
	"net"
	"sync"
)

const BUFFERSIZE = 2 * 1024 //we set the buffer size as 2k bytes

var Logger = log.Logger("thorchain-tsscom")

type Nodeinfo struct {
	ListenAddress string
	port          string
	wg            sync.WaitGroup
	send          chan Msg
	recv          chan Msg
}

type Msg struct {
	msg    []byte
	length int
}

func writedefaultconfig() error {
	viper.SetDefault("ListenAddress", "127.0.0.1")
	viper.SetDefault("Port", "5433")

	viper.SetConfigType("config")
	viper.SetConfigFile("Tssconfig.json")
	viper.AddConfigPath("./")
	viper.AddConfigPath("../")

	err := viper.WriteConfig()
	if err != nil {
		Logger.Errorf("error in write configure file %s\n", err.Error())
		return err
	}
	return nil

}

func NewNode(configname string) (Nodeinfo, error) {

	var node Nodeinfo
	viper.AddConfigPath("./")
	viper.SetConfigFile(configname)
	err := viper.ReadInConfig()
	if err != nil {
		Logger.Error("error in read the ")
		return node, err
	}

	node.ListenAddress = viper.Get("ListenAddress").(string)
	node.port = viper.Get("Port").(string)
	node.send = make(chan Msg, 10)
	node.recv = make(chan Msg, 10) // we should set it as the number of the participants

	return node, nil

}

func (self *Nodeinfo) Startserver() error {

	var tcpwg sync.WaitGroup

	// Accept a TCP connection
	localAddr := self.ListenAddress + ":" + self.port
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		Logger.Errorf("error in listening %s", err.Error())
		return err
	}
	Logger.Infof("Listen at %s\n", localAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		tcpwg.Add(1)
		go func(conn net.Conn) {
			session, err := yamux.Server(conn, nil)
			if err != nil {
				Logger.Infof("the connection is terminated %s", err.Error())
				return
			}
			tcpwg.Add(1)

			// Accept a stream

			stream, err := session.Accept()

			if err != nil {
				Logger.Infof("connection closed", err.Error())

				tcpwg.Done()
				tcpwg.Done()
			}

			// Listen for a message
			buf := make([]byte, BUFFERSIZE)
			n, err := stream.Read(buf)
			msg := Msg{
				buf,
				n,
			}
			if err != nil {
				Logger.Infof("stream may close")
				tcpwg.Done()
			}
			self.recv <- msg

		}(conn)
		tcpwg.Done()

	}
	//fixme we need a way to shutdown the server gracefully.

	return nil
}

func (self *Nodeinfo) StartNode()  {

	//self.SendMsg("127.0.0.1:5433")

	go self.Startserver()

	for {
		msg := <-self.recv
		recvmsg := msg.msg[:msg.length]
		Logger.Infof("mess>>>>%s\n", recvmsg)
	}

}

func (self *Nodeinfo) SendMsg(serverAddr string, bytemsg []byte) error {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return err
	}

	// Setup client side of yamux
	Logger.Infof("creating client session")
	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}

	// Open a new stream
	Logger.Infof("opening stream")
	stream, err := session.Open()
	if err != nil {
		return err
	}

	// Stream implements net.Conn
	_, err = stream.Write(bytemsg)
	if err != nil {
		Logger.Error("error in send the msg %s\n", err.Error())
	}
	stream.Close()
	return err
}
