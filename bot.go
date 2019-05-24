package main

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

type bot struct {
	mu      sync.Mutex
	cookie  string
	address string
	ws      *websocket.Conn
}

type message struct {
	Nick string `json:"nick"`
	Data string `json:"data"`
}

// TODO: auth

func newBot(cookie string) *bot {
	return &bot{cookie: cookie}
}

func main() {

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

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s/ws", b.address), nil)
	if err != nil {
		return err
	}

	b.ws = conn
	// TODO: start listening
	return nil
}

func (b *bot) close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ws == nil {
		return errors.New("connection already closed")
	}

	err := b.ws.Close()
	if err != nil {
		return err
	}

	b.ws = nil
	return nil
}
