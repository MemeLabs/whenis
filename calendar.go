package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
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

func getNextEvents(srv *calendar.Service, list *calendar.CalendarList) (*calendar.Event, error) {
	var event *calendar.Event
	startTime := time.Now().AddDate(0, 0, 100)
	now := time.Now()
	t := time.Now().Format(time.RFC3339)
	for _, item := range list.Items {
		e, err := srv.Events.List(item.Id).ShowDeleted(false).SingleEvents(true).TimeMin(t).MaxResults(10).OrderBy("startTime").Do()
		if err != nil {
			return nil, err
		}
		for _, item := range e.Items {
			t := eventStartTime(item)
			if t.Before(startTime) && t.After(now) {
				event = item
				startTime = t
				break
			}
		}
	}
	return event, nil
}

func getOngoingEvents(srv *calendar.Service, list *calendar.CalendarList) ([]*calendar.Event, error) {
	var events []*calendar.Event
	now := time.Now()
	startTime := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)
	t := time.Now().Format(time.RFC3339)
	for _, item := range list.Items {
		e, err := srv.Events.List(item.Id).ShowDeleted(false).SingleEvents(true).TimeMax(t).TimeMin(startTime).MaxResults(10).OrderBy("startTime").Do()
		if err != nil {
			return nil, err
		}
		for _, item := range e.Items {
			t := eventEndTime(item)
			if t.After(now) {
				events = append(events, item)
			}
		}
	}
	return events, nil
}

func searchString(srv *calendar.Service, list *calendar.CalendarList, query string, amount int64) ([]*calendar.Events, error) {
	var events []*calendar.Events
	t := time.Now().Format(time.RFC3339)
	for _, item := range list.Items {
		e, err := srv.Events.List(item.Id).ShowDeleted(false).SingleEvents(true).TimeMin(t).MaxResults(amount).OrderBy("startTime").Q(query).Do()
		if err != nil {
			return nil, err
		}
		if len(e.Items) > 0 {
			events = append(events, e)
		}
	}
	if len(events) > int(amount) {
		events = events[:int(amount)]
	}
	return events, nil
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
	b, err := ioutil.ReadFile("googleconfig.json")
	if err != nil {
		return nil, err
	}

	// If modifying these scopes, delete your previously saved token.json.
	newConfig, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope)
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
