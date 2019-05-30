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
	"github.com/jbpratt78/tmdb"
)

type bot struct {
	mu        sync.Mutex
	authToken string
	address   string
	conn      *websocket.Conn
	client    *tmdb.Client
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
	AuthToken  string `json:"auth_token"`
	Address    string `json:"address"`
	TmdbApiKey string `json:"tmdb_api_key"`
}

var configFile string

func main() {
	flag.Parse()

	config, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	bot := newBot(config)
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

func newBot(config *config) *bot {
	c := tmdb.New(config.TmdbApiKey)
	return &bot{authToken: ";jwt=" + config.AuthToken, client: c}
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
		return fmt.Errorf("handshake failed with status %v", resp)
	}

	b.conn = conn

	b.listen()

	return nil
}

func (b *bot) listen() {
	for {
		_, message, err := b.conn.ReadMessage()
		fmt.Println(string(message))
		if err != nil {
			log.Fatal(err)
		}
		m := parseMessage(message)

		if m.Contents != nil {
			if m.Type == "PRIVMSG" {
				fmt.Println("Received", m.Contents)
				err := b.send(m.Contents)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
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

func (b *bot) send(contents *contents) error {
	if b.conn == nil {
		return errors.New("no connection available")
	}

	query := strings.Fields(contents.Data)
	var response string

	if len(query) < 3 {
		response = "MiyanoBird"
	} else {
		t := query[0]
		switch {
		case t == "search":
			k := query[1]
			switch {
			case k == "movie":
				s, err := b.client.SearchMovie(query[2:])
				if err != nil {
					return err
				}
				response = handleMovieResult(s)
			case k == "tv":
				s, err := b.client.SearchTv(query[2:])
				if err != nil {
					return err
				}
				response = handleTvResult(s)
			case k == "keyword":
				s, err := b.client.SearchKeyword(query[2:])
				if err != nil {
					return err
				}
				response = handleKeywordResult(s)
			case k == "person":
				s, err := b.client.SearchPerson(query[2:])
				if err != nil {
					return err
				}
				response = handlePersonResult(s)
			case k == "collection":
				s, err := b.client.SearchCollection(query[2:])
				if err != nil {
					return err
				}
				response = handleCollectionResult(s)
			}
		case t == "discover":
		case t == "trending":
		default:
			response = "`ERROR: incorrect option`"
		}
	}

	defer fmt.Println("message sent")
	return b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`PRIVMSG {"nick": "%s", "data": "%s"}`, contents.Nick, response)))
}

func parseMessage(msg []byte) *message {

	received := string(msg)

	m := new(message)

	msgType := received[:strings.IndexByte(received, ' ')]

	m.Type = msgType

	m.Contents = parseContents(received, len(m.Type))

	return m
}

func parseContents(received string, length int) *contents {
	contents := contents{}
	json.Unmarshal([]byte(received[length:]), &contents)
	return &contents
}

func handleMovieResult(result *tmdb.SearchMovieResult) string {
	var out string
	fmt.Printf("%+v\n", result)
	for _, r := range result.Results {
		out += r.Title + " "
	}
	return out
}

func handleTvResult(result *tmdb.SearchTvResult) string {
	fmt.Printf("%+v\n", result)
	return ""
}

func handleKeywordResult(result *tmdb.SearchKeywordResult) string {
	fmt.Printf("%+v\n", result)
	return ""
}

func handlePersonResult(result *tmdb.SearchPersonResult) string {
	fmt.Printf("%+v\n", result)
	return ""
}

func handleCollectionResult(result *tmdb.SearchCollectionResult) string {
	fmt.Printf("%+v\n", result)
	return ""
}

func init() {
	flag.StringVar(&configFile, "config", "config.json", "location of config")
}
