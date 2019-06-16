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
	"strconv"
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
	calList    *calendar.CalendarList
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

	ticker := time.NewTicker(5 * time.Minute)
    go func() {
        for range ticker.C {
            bot.calList, err = getCalendars(bot.cal)
			if err != nil {
				log.Fatalf("Unable to get calendars: %v", err)
			}
        }
    }()

	for {
		bot.retriveCalendar(nil)
		bot.calList, err = getCalendars(bot.cal)
		if err != nil {
			log.Fatalf("Unable to get calendars: %v", err)
		}

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
	errc := make(chan error)
	go func() {
		for {
			_, message, err := b.conn.ReadMessage()
			if err != nil {
				errc <- fmt.Errorf("error trying to read message: %v", err)
			}
			m := parseMessage(message)

			if m.Contents != nil {
				if m.Type == "PRIVMSG" {
					go func() {
						err := b.answer(m.Contents, true)
						if err != nil {
							errc <- err
						}
					}()
				} else if strings.Contains(m.Contents.Data, "whenis") {
					go func() {
						err := b.answer(m.Contents, false)
						if err != nil {
							errc <- err
						}
					}()
				}
			}
		}
	}()
	err := <-errc
	return err
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

func (b *bot) answer(contents *contents, private bool) error {
	defer log.Println("===========================================================")
	prvt := "public"
	if private {
		prvt = "private"
	}
	log.Printf("received %s request from [%s]: \"%s\"", prvt, contents.Nick, contents.Data)
	searchText := contents.Data
	searchText = strings.Replace(searchText, "whenis", "", -1)
	searchText = strings.Trim(searchText, " ")
	searchText = strings.ToLower(searchText)

	if strings.Contains(searchText, "--next") || searchText == "" {
		return b.replyNextEvent(private, contents.Nick)
	} else if strings.Contains(searchText, "--multi") {
		log.Println("multi mode")
		return b.replyMultiSearch(searchText, contents.Nick)
	} else if strings.Contains(searchText, "help") {
		return b.replyHelp(contents.Nick)
	} else if strings.Contains(searchText, "--ongoing") {
		return b.replyOngoingEvents(private, contents.Nick)
	} else if strings.Contains(searchText, "mrmouton going to") {
		return b.sendSingleMsg(private, "never PepeLaugh", contents.Nick)
	}
	return b.replySingleSearch(searchText, private, contents.Nick)
}

func timeDiff(e *calendar.Event) time.Duration {
	return eventStartTime(e).Sub(time.Now())
}

func eventStartTime(e *calendar.Event) time.Time {
	date := e.Start.DateTime
	t, _ := time.Parse(time.RFC3339, date)
	if date == "" {
		date = e.Start.Date
		t, _ = time.Parse("2006-01-02", date)
	}
	return t
}

func eventEndTime(e *calendar.Event) time.Time {
	date := e.End.DateTime
	t, _ := time.Parse(time.RFC3339, date)
	if date == "" {
		date = e.End.Date
		t, _ = time.Parse("2006-01-02", date)
	}
	return t
}

func generateResponse(diff time.Duration, item *calendar.Event) string {
	var response string
	if diff.Round(time.Minute).Minutes() == 0 {
		response = fmt.Sprintf("%v is starting now", item.Summary)
	} else if diff.Minutes() < 0 {
		diff *= -1
		response = fmt.Sprintf("%v started %v ago", item.Summary, fmtDuration(diff))
	} else {
		response = fmt.Sprintf("%v is in %v", item.Summary, fmtDuration(diff))
	}
	return response
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

func (b *bot) multiSendMsg(messages []string, nick string) error {
	for _, message := range messages {
		err := b.sendMsg(message, true, nick)
		if err != nil {
			return err
		}
		time.Sleep(time.Millisecond * 500)
	}
	return nil
}

func (b *bot) sendMsg(message string, private bool, nick string) error {
	if private {
		log.Printf("sending private response: %s", message)
		err := b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`PRIVMSG {"nick": "%s", "data": "%s"}`, nick, message)))
		if err != nil {
			log.Printf(err.Error())
		}
		return err
	}
	// TODO: need mutex here
	b.lastPublic = time.Now()
	log.Printf("sending public response: %s", message)

	return b.conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`MSG {"data": "%s"}`, message)))
}

func (b *bot) replySingleSearch(search string, private bool, nick string) error {
	var response string
	var events []*calendar.Event
	eList, err := searchString(b.cal, b.calList, search, 1)
	if err != nil {
		b.retriveCalendar(err)
	}
	for _, event := range eList.Items {
		events = append(events, event)
	}

	if len(events) == 0 {
		response = "No upcoming events found."
	} else {
		event := events[0]
		response = generateResponse(timeDiff(event), event)
	}

	return b.sendSingleMsg(private, response, nick)
}

func (b *bot) replyNextEvent(private bool, nick string) error {
	var response string
	event, err := getNextEvents(b.cal, b.calList)
	if err != nil {
		b.retriveCalendar(err)
	}
	if event == nil {
		response = "No upcoming events found."
	} else {
		response = generateResponse(timeDiff(event), event)
	}
	return b.sendSingleMsg(private, response, nick)
}

func (b *bot) sendSingleMsg(private bool, response string, nick string) error {
	diff := time.Now().Sub(b.lastPublic)
	if diff.Seconds() >= 30 && !private && response != "No upcoming events found." {
		// TODO: need mutex here
		b.lastEmoji++
		if b.lastEmoji >= len(emojis) {
			b.lastEmoji = 0
		}
		return b.sendMsg(fmt.Sprintf("%s %s", emojis[b.lastEmoji], response), false, "")
	}
	return b.sendMsg(fmt.Sprintf("%s", response), true, nick)
}

func (b *bot) replyMultiSearch(search string, nick string) error {
	var responses []string
	var i int
	start := 0
	split := strings.Split(search, " ")
	for j, s := range split {
		if s == "--multi" {
			start = j
			break
		}
	}
	start += 2
	i, err := strconv.Atoi(split[start-1])
	if err != nil {
		if _, err := strconv.ParseFloat(split[start-1], 64); err == nil {
			return b.sendMsg("WeirdChamp", true, nick)
		}
		start--
		i = 5
	} else if i > 15 {
		i = 15
	} else if i < 0 {
		return b.sendMsg("Don't be so negative haHAA", true, nick)
	} else if i == 0 {
		return b.sendMsg("Nice one haHAA", true, nick)
	}
	search = strings.Join(split[start:], " ")

	events, err := searchString(b.cal, b.calList, search, int64(i))
	if err != nil {
		b.retriveCalendar(err)
	}

	if events == nil || len(events.Items) == 0 {
		return b.sendMsg("No upcoming events found.", true, nick)
	}

	for _, event := range events.Items {
		responses = append(responses, generateResponse(timeDiff(event), event))
	}
	return b.multiSendMsg(responses, nick)
}

func (b *bot) replyHelp(nick string) error {
	responses := []string{
		"`/msg whenis --help` to display this info",
		"`/msg whenis Formula 1` to search for an event (in this case F1)",
		"`/msg whenis --multi 5 Formula 1` to search for the next 5 F1 events",
		"`/msg whenis --next` to show the next scheduled event",
		"`/msg whenis --ongoing` to show a list of all ongoing events",
		"All of these also work in public chat, but some will only reply with private messages",
	}
	return b.multiSendMsg(responses, nick)
}

func (b *bot) replyOngoingEvents(private bool, nick string) error {
	var responses []string
	events, err := getOngoingEvents(b.cal, b.calList)
	if err != nil {
		b.retriveCalendar(err)
	}
	if events == nil || len(events) == 0 {
		return b.sendMsg("No upcoming events found.", true, nick)
	}
	for _, event := range events {
		responses = append(responses, generateResponse(timeDiff(event), event))
	}

	if len(responses) == 1 {
		return b.sendSingleMsg(private, responses[0], nick)
	}
	return b.multiSendMsg(responses, nick)
}

func (b *bot) retriveCalendar(err error) {
	if err != nil {
		log.Println(err)
	}
	b.cal, err = getCalendar()
	if err != nil {
		log.Fatalf("Unable to get calendar: %v", err)
	}
}
