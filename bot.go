package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MemeLabs/dggchat"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/calendar/v3"
)

type bot struct {
	failTimeout    time.Duration
	sgg            *dggchat.Session
	lastCheck      time.Time
	cal            *calendar.Service
	calList        *calendar.CalendarList
	emoteSwitch    bool
	lastPublicFail time.Time
	msgBuffer      chan msg
}

type msg struct {
	message   string
	private   bool
	recipient dggchat.User
}

type config struct {
	AuthToken            string `json:"auth_token"`
	Address              string `json:"address"`
	CalendarClientID     string `json:"calendar_client_ID"`
	CalendarClientSecret string `json:"calendar_client_secret"`
}

var configFile string

func main() {
	flag.Parse()

	config, err := readConfig()
	if err != nil {
		logrus.Fatal("failed to read config", err)
	}

	bot := newBot(config)

	logrus.Info("trying to establish connection...")
	err = bot.sgg.Open()
	if err != nil {
		logrus.Fatal("failed to connect", err)
	}
	logrus.Info("connected")

	bot.sgg.AddPMHandler(bot.onPM)
	bot.sgg.AddMessageHandler(bot.onMessage)
	bot.sgg.AddErrorHandler(onError)

	for {
		msg := <-bot.msgBuffer
		if msg.private {
			err := bot.sgg.SendPrivateMessage(msg.recipient.Nick, msg.message)
			if err != nil {
				logrus.Errorf("could not send private messate: %s", err)
			}
		} else {
			emote := "PepoG"
			if bot.emoteSwitch {
				emote = "PepoG:wide"
			}
			bot.emoteSwitch = !bot.emoteSwitch
			err := bot.sgg.SendMessage(fmt.Sprintf("%s %s", emote, msg.message))
			if err != nil {
				logrus.Errorf("could not send message: %s", err)
			}
		}
		time.Sleep(time.Millisecond * 450)
	}
}

func (b *bot) onPM(dm dggchat.PrivateMessage, s *dggchat.Session) {
	m := dggchat.Message{Sender: dm.User, Timestamp: dm.Timestamp, Message: dm.Message}
	b.answer(m, true)
}

func (b *bot) onMessage(m dggchat.Message, s *dggchat.Session) {
	if strings.HasPrefix(strings.TrimSpace(m.Message), "whenis") {
		b.answer(m, false)
	}
}

// noinspection GoUnusedParameter
func onError(e string, session *dggchat.Session) {
	logrus.Error("got error from chat", e)
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
	err = json.Unmarshal(bv, &c)
	if err != nil {
		return nil, err
	}

	return c, err
}

func newBot(config *config) *bot {
	var b bot
	b.failTimeout = time.Second * 30
	sgg, err := dggchat.New(";jwt=" + config.AuthToken)
	if err != nil {
		logrus.Fatalf("Unable to get connect to chat: %v", err)
	}
	b.sgg = sgg
	u, err := url.Parse(config.Address)
	if err != nil {
		logrus.Fatalf("[ERROR] can't parse url %v", err)
	}
	b.sgg.SetURL(*u)
	b.retrieveCalendar(nil)
	b.calList, err = getCalendars(b.cal)
	if err != nil {
		logrus.Fatalf("Unable to get calendars: %v", err)
	}
	b.msgBuffer = make(chan msg, 100)
	return &b
}

func (b *bot) answer(message dggchat.Message, private bool) {
	if time.Since(b.lastCheck).Minutes() > 5 {
		cals, err := getCalendars(b.cal)
		if err != nil {
			logrus.Errorf("Unable to get calendars: %v", err)
		} else {
			b.lastCheck = time.Now()
			b.calList = cals
		}
	}

	prvt := "public"
	if private {
		prvt = "private"
	}
	searchText := strings.TrimSpace(strings.Replace(message.Message, "whenis", "", -1))
	lowerText := strings.ToLower(searchText)
	logrus.Infof("received %s request from [%s]: %q", prvt, message.Sender.Nick, searchText)

	if strings.Contains(lowerText, "-next") || searchText == "" {
		b.replyNextEvent(private, message.Sender)
		return
	} else if strings.Contains(lowerText, "-multi") {
		b.replyMultiSearch(searchText, message.Sender)
		return
	} else if strings.Contains(lowerText, "help") {
		b.replyHelp(message.Sender)
		return
	} else if strings.Contains(lowerText, "-ongoing") {
		b.replyOngoingEvents(private, message.Sender)
		return
	} else if strings.Contains(lowerText, "-start") {
		b.setEvent(searchText, message.Sender)
		return
	} else if strings.Contains(lowerText, "-calendars") {
		b.listCalendars(message.Sender, private)
		return
	}
	b.replySingleSearch(searchText, private, message.Sender)
}

func timeDiff(e *calendar.Event) time.Duration {
	return time.Until(eventStartTime(e))
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
		response += fmt.Sprintf("%d %v", m, minuteString)
	}
	return response
}

func init() {
	flag.StringVar(&configFile, "config", "./config/config.json", "location of config")
}

func (b *bot) multiSendMsg(messages []string, user dggchat.User) {
	for _, message := range messages {
		b.sendMsg(message, true, user)
	}
}

func (b *bot) sendMsg(message string, private bool, user dggchat.User) {
	var m msg
	m.private = private
	m.recipient = user
	m.message = message

	b.msgBuffer <- m
}

func (b *bot) replySingleSearch(search string, private bool, user dggchat.User) {
	var response string
	events, err := query(b.cal, b.calList, search, 1)
	if err != nil {
		logrus.Errorf("error searching for event: %v", err)
		b.sendMsg("There was an error searching for your query. If this persists please contact SoMuchForSubtlety", true, user)
		return
	}

	if len(events) == 0 {
		ev, err := queryCalTitles(b.cal, b.calList, search, 1)
		if err != nil {
			logrus.Errorf("error searching for event: %v", err)
			b.sendMsg("There was an error searching for your query. If this persists please contact SoMuchForSubtlety", true, user)
			return
		}
		if ev == nil || len(ev.Items) == 0 {
			private = private || !b.canFailPublicly()
			b.sendMsg(fmt.Sprintf("No upcoming events found for '%s'", search), private, user)
			return
		}
		events = ev.Items
	}
	event := events[0]
	response = generateResponse(timeDiff(event), event)

	b.sendMsg(response, private, user)
}

func (b *bot) replyNextEvent(private bool, user dggchat.User) {
	var response string
	event, err := getNextEvent(b.cal, b.calList)
	if err != nil {
		logrus.Errorf("error searching for event: %v", err)
		b.sendMsg("There was an error searching for your query. If this persists please contact SoMuchForSubtlety", true, user)
		return
	}
	if event == nil {
		private = private || !b.canFailPublicly()
		response = "No upcoming events found."
	} else {
		response = generateResponse(timeDiff(event), event)
	}
	b.sendMsg(response, private, user)
}

func (b *bot) canFailPublicly() (response bool) {
	response = time.Since(b.lastPublicFail).Seconds() > b.failTimeout.Seconds()
	b.lastPublicFail = time.Now()
	return
}

func (b *bot) replyMultiSearch(search string, user dggchat.User) {
	var responses []string
	var i int
	start := 0
	split := strings.Fields(search)
	for j, s := range split {
		if s == "-multi" {
			start = j
			break
		}
	}
	start += 2
	i, err := strconv.Atoi(split[start-1])
	if err != nil {
		if _, err := strconv.ParseFloat(split[start-1], 64); err == nil {
			b.sendMsg("WeirdChamp", true, user)
			return
		}
		start--
		i = 5
	} else if i > 15 {
		i = 15
	} else if i < 0 {
		b.sendMsg("Don't be so negative haHAA", true, user)
		return
	} else if i == 0 {
		b.sendMsg("Nice one haHAA", true, user)
		return
	}
	search = strings.Join(split[start:], " ")

	events, err := query(b.cal, b.calList, search, int64(i))
	if err != nil {
		logrus.Errorf("error searching for event: %v", err)
		b.sendMsg("There was an error searching for your query. If this persists please contact SoMuchForSubtlety", true, user)
		return
	}

	if len(events) == 0 {
		b.sendMsg(fmt.Sprintf("No upcoming events found for '%s'", search), true, user)
		return
	}

	for _, event := range events {
		responses = append(responses, generateResponse(timeDiff(event), event))
	}
	b.multiSendMsg(responses, user)
}

func (b *bot) replyHelp(user dggchat.User) {
	responses := []string{
		"`/msg whenis -help` to display this info",
		"`/msg whenis Formula 1` to search for an event (in this case F1)",
		"`/msg whenis -multi 5 Formula 1` to search for the next 5 F1 events",
		"`/msg whenis -next` to show the next scheduled event",
		"`/msg whenis -ongoing` to show a list of all ongoing events",
		"`/msg whenis -start 20 Session Title` adds a session to the calendar with a duration of 20 minutes and the title 'Session Title' (abusing this will get you blacklisted)",
		"`/msg whenis -calendars` to get a list of active calendars",
		"All of these also work in public chat, but some will only reply with private messages",
	}
	b.multiSendMsg(responses, user)
}

func (b *bot) replyOngoingEvents(private bool, user dggchat.User) {
	var responses []string
	events, err := getOngoingEvents(b.cal, b.calList)
	if err != nil {
		logrus.Errorf("error searching for event: %v", err)
		b.sendMsg("There was an error searching for your query. If this persists please contact SoMuchForSubtlety", true, user)
		return
	}
	if len(events) == 0 {
		b.sendMsg("No ongoing events found.", true, user)
		return
	}
	for _, event := range events {
		if !regexp.MustCompile(`Week [0-9]{1,2} of [0-9]{4}`).Match([]byte(event.Summary)) {
			responses = append(responses, generateResponse(timeDiff(event), event))
		}
	}

	if len(responses) == 1 {
		b.sendMsg(responses[0], private, user)
		return
	}
	b.multiSendMsg(responses, user)
}

func (b *bot) retrieveCalendar(error) {
	var err error
	b.cal, err = getCalendar()
	if err != nil {
		logrus.Errorf("Unable to get calendar: %v", err)
	}
}

func (b *bot) setEvent(input string, user dggchat.User) {
	duration := time.Minute * 120
	var remainder string
	split := strings.Split(input, " ")

	if strings.ToLower(split[0]) != "-start" {
		b.sendMsg("invalid syntax", true, user)
		return
	}

	if i, err := strconv.Atoi(split[1]); err == nil {
		duration = time.Minute * time.Duration(int32(i))
		if len(split) >= 3 {
			remainder = strings.Join(split[2:], " ")
		}
	} else {
		if len(split) >= 2 {
			remainder = strings.Join(split[1:], " ")
		}
	}

	if duration.Minutes() >= 2880 || duration.Minutes() <= 0 {
		b.sendMsg("PepoBan", true, user)
		return
	}

	err := b.insertSession(remainder, user.Nick, duration)
	if err != nil {
		b.sendMsg("Error insertion your session, please contact SoMuchForSubtlety", true, user)
		return
	}
	b.sendMsg(fmt.Sprintf("Added '%v' with a duration of %v successfully.", remainder, fmtDuration(duration)), true, user)
}

func (b *bot) listCalendars(user dggchat.User, private bool) {
	var response string
	for _, calendarListEntry := range b.calList.Items {
		if !calendarListEntry.Primary {
			if calendarListEntry.SummaryOverride != "" {
				response += fmt.Sprintf(" `%s`", calendarListEntry.SummaryOverride)
			} else {
				response += fmt.Sprintf(" `%s`", calendarListEntry.Summary)
			}
		}
	}
	b.sendMsg(response, private, user)
}
