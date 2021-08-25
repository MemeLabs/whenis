package main

import (
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	googlecal "google.golang.org/api/calendar/v3"

	"github.com/MemeLabs/whenis/pkg/bot"
	"github.com/MemeLabs/whenis/pkg/calendar"
	"github.com/MemeLabs/whenis/pkg/chat"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
)

var googleCfgLocation = flag.String("config", "", "the location of you google oauth config")

func main() {
	flag.Parse()
	logrus.SetLevel(logrus.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *googleCfgLocation == "" {
		logrus.Fatal("missing oauth config (-h for details)")
	}

	chat, err := chat.Connect(ctx, "wss://chat.strims.gg/ws", os.Getenv("STRIMS_JWT"))
	if err != nil {
		logrus.Fatal(err)
	}

	googleCfgBytes, err := ioutil.ReadFile(*googleCfgLocation)
	if err != nil {
		logrus.Fatal(err)
	}
	cfg, err := google.ConfigFromJSON(googleCfgBytes, googlecal.CalendarScope)
	if err != nil {
		logrus.Fatal(err)
	}
	cal, err := calendar.NewCalendar(ctx, cfg, os.Getenv("CAL_REFRESH_TOKEN"))
	if err != nil {
		logrus.Fatal(err)
	}
	bot.NewBotForChat(ctx, chat, "whenis", cal)

	signalchan := make(chan os.Signal, 2)
	signal.Notify(signalchan, syscall.SIGTERM, os.Interrupt)
	<-signalchan
	cancel()
}
