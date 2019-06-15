package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/gorilla/websocket"
)

type bot struct {
	mu         sync.Mutex
	authToken  string
	address    string
	conn       *websocket.Conn
	cal        *calendar.Service
	lastEmoji  int
	lastPublic time.Time
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
	AuthToken            string `json:"auth_token"`
	Address              string `json:"address"`
	CalendarClientID     string `json:"calendar_client_ID"`
	CalendarClientSecret string `json:"calendar_client_secret"`
}

var configFile string
var emojis = [...]string{"🎬", "📺", "🍿", "📽️", "🎞", "🎥"}

func main() {
	defer log.Println("terminating")
	flag.Parse()
	logFile, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer logFile.Close()
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)

	config, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	bot := newBot(config)
	if err = bot.setAddress(config.Address); err != nil {
		log.Fatal(err)
	}

	cal, err := getCalendar()
	if err != nil {
		log.Fatalf("Unable to get calendar: %v", err)
	}
	getCalendars(cal)
	for {
		bot.cal = cal
		bot.lastPublic = time.Now().AddDate(0, 0, -1)

		err = bot.connect()
		if err != nil {
			log.Println(err)
		}
		bot.close()
		time.Sleep(time.Second * 5)
		log.Println("trying to reestablish connection")
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
	return &bot{authToken: ";jwt=" + config.AuthToken}
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
		return fmt.Errorf("handshake failed with status: %v", resp)
	}
	log.Println("Connection established.")

	b.conn = conn

	err = b.listen()
	if err != nil {
		return err
	}
	return nil
}

func (b *bot) listen() error {
	for {
		_, message, err := b.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("error trying to read message: %v", err)
		}
		m := parseMessage(message)

		if m.Contents != nil {
			if m.Type == "PRIVMSG" {
				log.Printf("Received: { %s: %s }", m.Contents.Nick, m.Contents.Data)
				err := b.send(m.Contents, true)
				if err != nil {
					log.Println(err)
				}
			} else if strings.Contains(m.Contents.Data, "whenis") {
				log.Printf("Received: { %s: %s }", m.Contents.Nick, m.Contents.Data)
				err := b.send(m.Contents, false)
				if err != nil {
					log.Println(err)
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
		return fmt.Errorf("error trying to close connection: %v", err)
	}

	b.conn = nil
	return nil
}

func (b *bot) send(contents *contents, private bool) error {
	var response string
	searchText := contents.Data
	searchText = strings.Replace(searchText, "whenis", "", -1)
	searchText = strings.Trim(searchText, " ")

	if strings.ToLower(searchText) == "ligma" && !private {
		return b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`MSG {"data": "ligma balls %s"}`, contents.Nick)))
	}

	events, err := searchString(b.cal, searchText, 1)
	if err != nil {
		return err
	}
	if len(events.Items) == 0 {
		response = "No upcoming events found."
	} else {
		for _, item := range events.Items {
			date := item.Start.DateTime
			t, _ := time.Parse(time.RFC3339, date)
			if date == "" {
				date = item.Start.Date
				t, _ = time.Parse("2006-01-02", date)
			}
			diff := t.Sub(time.Now())
			if diff.Round(time.Minute).Minutes() == 0 {
				response = fmt.Sprintf("%v is starting now", item.Summary)
			} else if diff.Minutes() < 0 {
				diff *= -1
				response = fmt.Sprintf("%v started %v ago", item.Summary, fmtDuration(diff))
			} else {
				response = fmt.Sprintf("%v is in %v", item.Summary, fmtDuration(diff))
			}
		}
	}

	if b.conn == nil {
		return errors.New("no connection available")
	}
	defer log.Println("===============================")
	b.lastEmoji++
	if b.lastEmoji >= len(emojis) {
		b.lastEmoji = 0
	}
	diff := time.Now().Sub(b.lastPublic)
	if diff.Seconds() >= 30 && !private && response != "No upcoming events found." {
		log.Printf("sending public response: %s", response)
		b.lastPublic = time.Now()
		return b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`MSG {"data": "%s %s"}`, emojis[b.lastEmoji], response)))
	}
	log.Printf("sending private response: %s", response)
	return b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`PRIVMSG {"nick": "%s", "data": "%s %s"}`, contents.Nick, emojis[b.lastEmoji], response)))
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

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	hours := h % 24
	days := (h - hours) / 24
	d -= h * time.Hour
	m := d / time.Minute
	response := ""
	if days > 0 {
		dayString := "days"
		sep := ","
		if days == 1 {
			dayString = "day"
		}
		if m == 0 || hours == 0 {
			sep = " and"
		}
		if m == 0 && hours == 0 {
			sep = ""
		}
		response += fmt.Sprintf("%d %v%v ", days, dayString, sep)
	}
	if hours > 0 {
		hourString := "hours"
		sep := "and"
		if hours == 1 {
			hourString = "hour"
		}
		if m == 0 {
			sep = ""
		}
		response += fmt.Sprintf("%d %v %v ", hours, hourString, sep)
	}
	if m > 0 {
		minuteString := "minutes"
		if m == 1 {
			minuteString = "minute"
		}
		response += fmt.Sprintf("%d %v ", m, minuteString)
	}
	return response
}

func init() {
	flag.StringVar(&configFile, "config", "config.json", "location of config")
}
