package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

type result struct {
	eventList *calendar.Events
	er        error
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "./config/token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// maybe rundundant, could be replaced with searchString()
//noinspection GoNilness
func getNextEvent(srv *calendar.Service, list *calendar.CalendarList) (*calendar.Event, error) {
	type eventStr struct {
		ev []*calendar.Event
		er error
	}
	eventc := make(chan eventStr)

	var firstEvent *calendar.Event

	counter := len(list.Items)
	startTime := time.Now().AddDate(0, 0, 100)
	now := time.Now()
	t := now.Format(time.RFC3339)

	for _, item := range list.Items {
		go func(itm *calendar.CalendarListEntry) {
			e, err := srv.Events.List(itm.Id).ShowDeleted(false).SingleEvents(true).TimeMin(t).MaxResults(3).OrderBy("startTime").Do()
			eventc <- eventStr{ev: e.Items, er: err}
		}(item)
	}

	for i := 0; i < counter; i++ {
		res := <-eventc
		if res.er != nil {
			return nil, res.er
		}
		for _, event := range res.ev {
			t := eventStartTime(event)
			if t.Before(startTime) && t.After(now) && !regexp.MustCompile(`Week [0-9]{1,2} of [0-9]{4}`).Match([]byte(event.Summary)) {
				firstEvent = event
				startTime = t
				break
			}
		}
	}
	return firstEvent, nil
}

func getOngoingEvents(srv *calendar.Service, list *calendar.CalendarList) ([]*calendar.Event, error) {
	type chanContainer struct {
		eventList *calendar.Events
		er        error
	}
	var result []*calendar.Event
	counter := len(list.Items)
	eventc := make(chan chanContainer)
	now := time.Now()
	startTime := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)
	t := time.Now().Format(time.RFC3339)
	for _, item := range list.Items {
		go func(itm *calendar.CalendarListEntry) {
			e, err := srv.Events.List(itm.Id).ShowDeleted(false).SingleEvents(true).TimeMax(t).TimeMin(startTime).MaxResults(15).OrderBy("startTime").Do()
			eventc <- chanContainer{eventList: e, er: err}
		}(item)
	}
	for i := 0; i < counter; i++ {
		res := <-eventc
		if res.er != nil {
			return nil, res.er
		}
		for _, item := range res.eventList.Items {
			t := eventEndTime(item)
			if t.After(now) {
				result = append(result, item)
			}
		}
	}
	return result, nil
}

// returns a list of all events that are ongoing or happenign in the future, sorted by starting time
func query(srv *calendar.Service, list *calendar.CalendarList, query string, amount int64) ([]*calendar.Event, error) {
	var finalList []*calendar.Event

	counter := len(list.Items)
	eventc := make(chan result)
	t := time.Now().Format(time.RFC3339)
	for _, item := range list.Items {
		go func(itm *calendar.CalendarListEntry) {
			e, err := srv.Events.List(itm.Id).ShowDeleted(false).SingleEvents(true).TimeMin(t).MaxResults(amount).OrderBy("startTime").Q(query).Do()
			if err != nil {
				eventc <- result{eventList: nil, er: err}
			}
			eventc <- result{eventList: e, er: nil}
		}(item)
	}
	for i := 0; i < counter; i++ {
		res := <-eventc
		if res.er != nil {
			return nil, res.er
		}
		finalList = append(finalList, res.eventList.Items...)
	}
	sort.Slice(finalList, func(i, j int) bool { return eventStartTime(finalList[i]).Before(eventStartTime(finalList[j])) })
	if len(finalList) > int(amount) {
		finalList = finalList[:int(amount)]
	}
	return finalList, nil
}

func queryCalTitles(srv *calendar.Service, list *calendar.CalendarList, query string, amount int64) (*calendar.Events, error) {
	eventc := make(chan result)
	t := time.Now().Format(time.RFC3339)
	counter := len(list.Items)
	for _, cal := range list.Items {
		go func(calendar *calendar.CalendarListEntry) {
			if !calendar.Primary {
				if strings.Contains(strings.ToLower(calendar.SummaryOverride), strings.ToLower(query)) || strings.Contains(strings.ToLower(calendar.Summary), strings.ToLower(query)) {
					e, err := srv.Events.List(calendar.Id).ShowDeleted(false).SingleEvents(true).TimeMin(t).MaxResults(amount).OrderBy("startTime").Do()
					eventc <- result{eventList: e, er: err}
					return
				}
			}
			eventc <- result{}
		}(cal)
	}
	for i := 0; i < counter; i++ {
		res := <-eventc
		if res.er != nil {
			return nil, res.er
		} else if res.eventList != nil && len(res.eventList.Items) > 0 {
			return res.eventList, nil
		}
	}
	return nil, nil
}

func queryPrimary(srv *calendar.Service, query string, amount int64) (*calendar.Event, error) {
	e, err := srv.Events.List("primary").ShowDeleted(false).SingleEvents(true).MaxResults(amount).OrderBy("startTime").Q(query).Do()
	if err != nil {
		return nil, err
	}
	if len(e.Items) < 1 {
		return nil, nil
	}
	return e.Items[0], nil
}

func getCalendars(srv *calendar.Service) (*calendar.CalendarList, error) {
	list, err := srv.CalendarList.List().Do()
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return list, nil
}

func getCalendar() (*calendar.Service, error) {
	b, err := ioutil.ReadFile("./config/googleconfig.json")
	if err != nil {
		return nil, err
	}

	// If modifying these scopes, delete your previously saved token.json.
	newConfig, err := google.ConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		return nil, err
	}
	client := getClient(newConfig)

	srv, err := calendar.New(client)
	if err != nil {
		return nil, err
	}
	return srv, nil
}

func (b *bot) insertSession(title string, nick string, timeOffset time.Duration) error {
	event := &calendar.Event{
		Summary:     title,
		Description: nick,
		Start: &calendar.EventDateTime{
			DateTime: time.Now().Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: time.Now().Add(timeOffset).Format(time.RFC3339),
		},
	}
	err := b.removeSession(nick)
	if err != nil {
		return err
	}
	log.Printf("Inserting session \"%v\" for [%v]", title, nick)
	_, err = b.cal.Events.Insert("primary", event).Do()
	return err
}

func (b *bot) removeSession(nick string) error {
	log.Printf("removing session \"%v\"", nick)
	result, err := queryPrimary(b.cal, nick, 1)
	if err != nil {
		return err
	} else if result == nil{
		return nil
	}
	return b.cal.Events.Delete("primary", result.Id).Do()
}
