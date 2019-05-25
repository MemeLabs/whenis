package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type bot struct {
	mu        sync.Mutex
	authToken string
	address   string
	conn      *websocket.Conn
}

type message struct {
	Type     string `json:"type"`
	Contents *contents
}

type contents struct {
	Nick      string `json:"nick"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

type config struct {
	AuthToken string `json:"auth_token"`
	Address   string `json:"address"`
}

var configFile string

func main() {
	flag.Parse()

	config, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	bot := newBot(config.AuthToken)
	if err = bot.setAddress(config.Address); err != nil {
		log.Fatal(err)
	}

	err = bot.connect()
	if err != nil {
		bot.close()
		log.Fatal(err)
	}
}

func readConfig() (*config, error) {
	file, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bv, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var c *config
	c = new(config)

	err = json.Unmarshal(bv, &c)
	if err != nil {
		return nil, err
	}

	return c, err
}

func newBot(authToken string) *bot {
	return &bot{authToken: ";jwt=" + authToken}
}

func (b *bot) setAddress(url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if url == "" {
		return errors.New("url address not supplied")
	}

	b.address = url
	return nil
}

func (b *bot) connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	header := http.Header{}
	header.Add("Cookie", fmt.Sprintf("authtoken=%s", b.authToken))

	conn, resp, err := websocket.DefaultDialer.Dial(b.address, header)
	if err != nil {
		return errors.New(fmt.Sprintf("handshake failed with status %v", resp))
	}

	b.conn = conn

	b.listen()

	return nil
}

func (b *bot) listen() {
	for {
		_, message, err := b.conn.ReadMessage()
		if err != nil {
			log.Fatal(err)
		}

		m, err := parseMessage(message)
		if err != nil {
			log.Println("FAILED TO PARSE:", string(message))
			continue
		}

		fmt.Printf("%+v\n", *m.Contents)

		continue
	}
}

func (b *bot) close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == nil {
		return errors.New("connection already closed")
	}

	err := b.conn.Close()
	if err != nil {
		return err
	}

	b.conn = nil
	return nil
}

func init() {
	flag.StringVar(&configFile, "config", "config.json", "location of config")
}

func parseMessage(msg []byte) (*message, error) {

	received := string(msg)

	m := new(message)

	msgType := received[:strings.IndexByte(received, ' ')]

	m.Type = msgType

	m.Contents = after(received, len(m.Type))

	return m, nil
}

func after(received string, length int) *contents {
	contents := contents{}
	json.Unmarshal([]byte(received[length:]), &contents)
	return &contents
}
