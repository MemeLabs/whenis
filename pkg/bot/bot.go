package bot

import (
	"context"
	"fmt"
	"time"

	googlecal "google.golang.org/api/calendar/v3"

	"github.com/MemeLabs/whenis/pkg/calendar"
	"github.com/MemeLabs/whenis/pkg/chat"
	"github.com/sirupsen/logrus"
)

type Bot struct {
	cal  *calendar.Calendar
	chat *chat.Chat

	emote bool
	name  string
}

func NewBotForChat(ctx context.Context, chat *chat.Chat, name string, cal *calendar.Calendar) *Bot {
	bot := &Bot{
		chat: chat,
		name: name,
		cal:  cal,
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
			if msg.Sender == bot.name || !msg.Mentions(bot.name) {
				continue
			}
			bot.processMessage(msg)
		}
	}
}

func (bot *Bot) processMessage(msg chat.Message) {
	event, err := bot.cal.Query(msg.WithoutNick(bot.name), 1)
	if err != nil {
		logrus.Error("failed to handle request", err)
		bot.Send(err.Error())
		return
	}

	if len(event) == 0 {
		bot.Send("idk SHRUG")
		return
	}

	bot.Send(generateResponse(event[0]))
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

func (b *Bot) Send(msg string) {
	if b.emote {
		msg = "TANTIES " + msg
	}
	// TODO: race
	b.emote = !b.emote
	err := b.chat.Send(msg)
	if err != nil {
		logrus.Error("failed to send msg", err)
	}
}
