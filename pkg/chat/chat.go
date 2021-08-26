package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Chat struct {
	conn *websocket.Conn

	MessageChan chan Message
}

type outboundMsg struct {
	Data string  `json:"data"`
	Nick Chatter `json:"nick"`
}

func (c *Chat) SendPriv(recipient Chatter, message string) error {
	msg := outboundMsg{Data: message, Nick: recipient}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	msgBytes = append([]byte("PRIVMSG "), msgBytes...)
	err = c.conn.WriteMessage(websocket.TextMessage, msgBytes)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *Chat) Send(message string) error {
	msg := outboundMsg{Data: message}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	msgBytes = append([]byte("MSG "), msgBytes...)
	err = c.conn.WriteMessage(websocket.TextMessage, msgBytes)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func Connect(ctx context.Context, wsUrl, jwt string) (*Chat, error) {
	chat := &Chat{
		MessageChan: make(chan Message, 10),
	}

	go chat.connectLoop(ctx, wsUrl, jwt)

	return chat, nil
}

func (c *Chat) connectLoop(ctx context.Context, wsUrl, jwt string) {
	var backoff time.Duration

	for {
		conn, _, err := websocket.DefaultDialer.Dial(wsUrl, http.Header{"Cookie": []string{"jwt=" + jwt}})
		if err != nil {
			logrus.Error("failed to connect to WS", err)
			backoff = min(backoff*2, time.Second*10)
		} else {
			backoff = time.Millisecond * 10
			c.conn = conn
			if err = c.readLoop(ctx); err != nil {
				logrus.Error("read loop failed:", err)
			}
			if err = c.conn.Close(); err != nil {
				logrus.Error("failed to close WS:", err)
			}
		}

		select {
		case <-ctx.Done():
			logrus.Info("exiting")
			return
		case <-time.After(backoff):
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}

	return b
}

func (c *Chat) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// TODO: read with context
			// corrently the provided context only gets checked after getting a message,
			// so if no messages arrive, this will block forever
			_, msg, err := c.conn.ReadMessage()
			if err != nil {
				return fmt.Errorf("failed to read message: %w", err)
			}
			parts := bytes.SplitN(msg, []byte(" "), 2)
			if len(parts) != 2 {
				logrus.Warn("unexpected message:", string(msg))
				continue
			}
			jsonBytes := parts[1]
			switch string(parts[0]) {
			case "ERR":
				logrus.Error("got error from chat: ", string(parts[1]))
			case "MSG":
				// this will leak goroutines if noone is listening
				go c.handleMsg(jsonBytes, false)
			case "PRIVMSG":
				go c.handleMsg(jsonBytes, true)
			default:
				// don't care
			}
		}
	}
}

func (c *Chat) handleMsg(msgBytes []byte, priv bool) {
	var msg Message
	err := json.Unmarshal(msgBytes, &msg)
	if err != nil {
		logrus.Error("failed to unmarshal message", err)
		return
	}
	msg.Raw = msgBytes
	msg.Private = priv

	c.MessageChan <- msg
}
