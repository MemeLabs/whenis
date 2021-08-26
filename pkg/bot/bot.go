package bot

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	googlecal "google.golang.org/api/calendar/v3"

	"github.com/MemeLabs/whenis/pkg/calendar"
	"github.com/MemeLabs/whenis/pkg/chat"
	"github.com/sirupsen/logrus"
)

type Bot struct {
	cal  *calendar.Calendar
	chat *chat.Chat

	lastMsg string
	emote   bool

	name chat.Chatter

	ongoingAdditions map[chat.Chatter]*eventEntry
}

func NewBotForChat(ctx context.Context, c *chat.Chat, name string, cal *calendar.Calendar) *Bot {
	bot := &Bot{
		chat:             c,
		name:             chat.Chatter(name),
		cal:              cal,
		ongoingAdditions: make(map[chat.Chatter]*eventEntry),
	}

	go bot.handleMessages(ctx)

	return bot
}

func (bot *Bot) handleMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-bot.chat.MessageChan:
			if msg.Sender == bot.name || (!msg.Mentions(bot.name) && !msg.Private) {
				continue
			}
			bot.process(msg)
		}
	}
}

func (bot *Bot) process(msg chat.Message) {
	f := flag.NewFlagSet("whenis", flag.ContinueOnError)
	list := f.Bool("list", false, "list all available calendars")
	add := f.Bool("add", false, "add an event")
	abort := f.Bool("abort", false, "stop an action")
	_ = f.Parse(strings.Split(msg.WithoutNick(bot.name), " "))

	if *abort {
		logrus.WithField("chatter", msg.Sender).Info("aborted action")
		delete(bot.ongoingAdditions, msg.Sender)
		bot.SendPriv(msg.Sender, "PepOk")
		return
	}
	if _, ok := bot.ongoingAdditions[msg.Sender]; ok {
		bot.continueAddingEvent(msg)
		return
	}

	if *add {
		logrus.WithField("chatter", msg.Sender).Info("starting to add event")
		bot.SendPriv(msg.Sender, "what should the title be? (you can stop adding the event any time using `-abort`)")
		bot.ongoingAdditions[msg.Sender] = &eventEntry{}
		return
	}

	if *list {
		bot.sendList(msg)
		return
	}

	bot.simpleQuery(msg)
}

func (bot *Bot) sendList(msg chat.Message) {
	logrus.WithField("chatter", msg.Sender).Info("got a list request")
	var resp string

	for _, name := range bot.cal.List() {
		resp += fmt.Sprintf("`%s` ", name)
	}

	if msg.Private {
		bot.SendPriv(msg.Sender, resp)
	} else {
		bot.Send(resp)
	}
}

type eventEntry struct {
	title          string
	searchKeywords string
	time           time.Time
	duration       time.Duration
}

func (bot *Bot) continueAddingEvent(msg chat.Message) {
	// TODO: locking, logging

	e, ok := bot.ongoingAdditions[msg.Sender]
	if !ok {
		return
	}

	msg.Data = strings.TrimSpace(msg.WithoutNick(bot.name))

	if e.title == "" {
		e.title = msg.Data
		if e.title != "" {
			bot.SendPriv(msg.Sender, "what search keywords should the event have?")
		} else {
			bot.SendPriv(msg.Sender, "invalid title, please try again")
		}
		return
	}
	if e.searchKeywords == "" {
		e.searchKeywords = msg.Data
		if e.searchKeywords == "" {
			e.searchKeywords = "-"
		}
		bot.SendPriv(msg.Sender, "at what time does it start? acceptable formats are `2006-01-02T15:04:05Z` (RFC3339), `1630006096` (unix) or `in 2h5m`")
		return
	}
	var err error
	if e.time.IsZero() {
		e.time, err = time.Parse(time.RFC3339, msg.Data)
		if err == nil {
			if e.time.Before(time.Now()) {
				bot.SendPriv(msg.Sender, "thats in the past FeelsPepoMan ")
				e.time = time.Time{}
				return
			}
			bot.SendPriv(msg.Sender, "how long will the event last? provide the duration in the format `2h5m`")
			return
		}
		unix, err := strconv.ParseInt(msg.Data, 10, 64)
		if err == nil {
			e.time = time.Unix(unix, 0)
			if e.time.Before(time.Now()) {
				bot.SendPriv(msg.Sender, "thats in the past FeelsPepoMan ")
				e.time = time.Time{}
				return
			}
			bot.SendPriv(msg.Sender, "how long will the event last? provide the duration in the format `2h5m`")
			return
		}

		duration, err := time.ParseDuration(strings.TrimPrefix(msg.Data, "in "))
		if err == nil {
			e.time = time.Now().Add(duration)
			bot.SendPriv(msg.Sender, "how long will the event last? provide the duration in the format `2h5m`")
			return
		}

		bot.SendPriv(msg.Sender, "I could not understand that, please try again.")
		return
	}
	if e.duration == 0 {
		e.duration, err = time.ParseDuration(msg.Data)
		if err == nil {
			err := bot.cal.AddEvent(string(msg.Sender), e.title, e.searchKeywords, e.time, e.duration)
			if err == nil {
				logrus.WithFields(logrus.Fields{
					"chatter":     msg.Sender,
					"title":       e.title,
					"start":       e.time,
					"end":         e.time.Add(e.duration),
					"description": e.searchKeywords,
				}).Info("added event")
				bot.SendPriv(msg.Sender, "noted PepoG")
			} else {
				logrus.Error("failed to add event", err)
				bot.SendPriv(msg.Sender, fmt.Sprintf("could not add event %v", err))
			}
			delete(bot.ongoingAdditions, msg.Sender)
			return
		}
		e.duration = 0
		bot.SendPriv(msg.Sender, "I could not understand that, please try again.")
		return
	}
}

func (bot *Bot) simpleQuery(msg chat.Message) {
	logrus.WithFields(logrus.Fields{
		"chatter": msg.Sender,
		"query":   msg.WithoutNick(bot.name),
		"private": msg.Private,
	}).Info("got request")

	if strings.Contains(msg.Data, "gorilla warfare") {
		bot.navySeal(msg.Sender)
		return
	}

	events, err := bot.cal.Query(msg.WithoutNick(bot.name), 1)
	if err != nil {
		logrus.Error("failed to handle request", err)
		bot.Send(err.Error())
		return
	}
	var event *googlecal.Event
	if len(events) == 0 {
		event, err = bot.cal.QueryCalendarTitles(msg.WithoutNick(bot.name))
		if err != nil {
			logrus.Error("failed to handle request", err)
			bot.Send(err.Error())
			return
		}
	} else {
		event = events[0]
	}

	if event == nil {
		if bot.emote {
			bot.Send("idk SHRUG")
		} else {
			bot.Send("idk TANTIES")
		}
		bot.emote = !bot.emote
		return
	}

	if msg.Private {
		bot.SendPriv(msg.Sender, generateResponse(event))
	} else {
		bot.Send(generateResponse(event))
	}
}

func (bot *Bot) navySeal(nick chat.Chatter) {
	parts := strings.Split("What the fuck did you just fucking say about me, you little bitch? I'll have you know I graduated top of my class in the Navy Seals, and I've been involved in numerous secret raids on Al-Quaeda, and I have over 300 confirmed kills. I am trained in gorilla warfare and I'm the top sniper in the entire US armed forces. You are nothing to me but just another target. I will wipe you the fuck out with precision the likes of which has never been seen before on this Earth, mark my fucking words. You think you can get away with saying that shit to me over the Internet? Think again, fucker. As we speak I am contacting my secret network of spies across the USA and your IP is being traced right now so you better prepare for the storm, maggot. The storm that wipes out the pathetic little thing you call your life. You're fucking dead, kid. I can be anywhere, anytime, and I can kill you in over seven hundred ways, and that's just with my bare hands. Not only am I extensively trained in unarmed combat, but I have access to the entire arsenal of the United States Marine Corps and I will use it to its full extent to wipe your miserable ass off the face of the continent, you little shit. If only you could have known what unholy retribution your little \"clever\" comment was about to bring down upon you, maybe you would have held your fucking tongue. But you couldn't, you didn't, and now you're paying the price, you goddamn idiot. I will shit fury all over you and you will drown in it. You're fucking dead, kiddo.", " ")
	logrus.Infof("%s is a navy seal", nick)
	for _, part := range parts {
		bot.SendPriv(nick, part)
		time.Sleep(time.Millisecond * 10)
	}
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

func generateResponse(event *googlecal.Event) string {
	diff := time.Until(calendar.StartTime(event))
	var response string
	if diff.Round(time.Minute).Minutes() == 0 {
		response = fmt.Sprintf("%v is starting now", event.Summary)
	} else if diff.Minutes() < 0 {
		diff *= -1
		response = fmt.Sprintf("%v started %v ago", event.Summary, fmtDuration(diff))
	} else {
		response = fmt.Sprintf("%v is in %v", event.Summary, fmtDuration(diff))
	}
	return response
}

func (b *Bot) SendPriv(recipient chat.Chatter, msg string) {
	err := b.chat.SendPriv(recipient, msg)
	if err != nil {
		logrus.Error("failed to send msg", err)
	}
}

func (b *Bot) Send(msg string) {
	// TODO: locking
	if b.lastMsg == msg {
		msg += " TANTIES"
	}
	b.lastMsg = msg
	err := b.chat.Send(msg)
	if err != nil {
		logrus.Error("failed to send msg", err)
	}
}
