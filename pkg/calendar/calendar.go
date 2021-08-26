package calendar

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MemeLabs/whenis/pkg/util"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type Calendar struct {
	*calendar.Service
	sync.RWMutex

	calListEtag  string
	lastRefresh  time.Time
	subCalendars []*calendar.CalendarListEntry
}

func NewCalendar(ctx context.Context, googleCfg *oauth2.Config, refreshToken string) (*Calendar, error) {
	client := googleCfg.Client(context.Background(), &oauth2.Token{
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
	})

	cal, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return &Calendar{Service: cal}, nil
}

func (cal *Calendar) List() []string {
	cal.Refresh()
	cal.RLock()
	defer cal.RUnlock()

	var names []string
	for _, c := range cal.subCalendars {
		if c.Primary || strings.HasPrefix(c.Summary, "http") {
			continue
		}
		names = append(names, c.Summary)
	}

	return names
}

func (cal *Calendar) Refresh() {
	cal.Lock()
	defer cal.Unlock()
	// TODO: adjust cache interval
	if time.Now().After(cal.lastRefresh.Add(time.Minute * 5)) {
		updated, err := cal.CalendarList.List().IfNoneMatch(cal.calListEtag).Do()
		if err != nil {
			if !googleapi.IsNotModified(err) {
				logrus.Error("failed to refresh calendar list", err)
			}
		} else {
			if updated.NextPageToken != "" {
				logrus.Error("TODO: implent paging for calendar list sync")
			}
			cal.subCalendars = updated.Items
			cal.calListEtag = updated.Etag
		}
	}
}

func (cal *Calendar) CalendarIDs() []string {
	cal.Refresh()
	cal.RLock()
	defer cal.RUnlock()

	var calIds []string
	for _, c := range cal.subCalendars {
		calIds = append(calIds, c.Id)
	}

	return calIds
}

func (cal *Calendar) CalendarIDsMatching(query string) []string {
	cal.Refresh()
	cal.RLock()
	defer cal.RUnlock()

	var calIds []string
	for _, c := range cal.subCalendars {
		if !c.Primary && (util.ContainsFold(c.SummaryOverride, query) || util.ContainsFold(c.Summary, query)) {
			calIds = append(calIds, c.Id)
		}
	}
	return calIds
}

func (cal *Calendar) OngoingEvents() ([]*calendar.Event, error) {
	startTime := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)
	endTime := time.Now().Format(time.RFC3339)
	candidates, err := cal.QueryCalendars("", func(c *calendar.EventsListCall) { c.TimeMax(endTime).TimeMin(startTime) })
	if err != nil {
		return nil, fmt.Errorf("failed to get ongoing events: %w", err)
	}

	var results []*calendar.Event
	for _, event := range candidates {
		if EndTime(event).After(time.Now()) {
			results = append(results, event)
		}
	}

	return results, nil
}

func (cal *Calendar) multiFast(calendars []string, mods ...func(c *calendar.EventsListCall)) ([]*calendar.Event, error) {
	resChan := make(chan []*calendar.Event)
	errChan := make(chan error)

	for _, id := range calendars {
		id := id
		go func() {
			q := cal.Events.List(id)
			for _, mod := range mods {
				mod(q)
			}
			events, err := cal.executeListCall(q, id)
			if err != nil {
				errChan <- err
			} else {
				resChan <- events
			}
		}()
	}

	var resErr error
	var res []*calendar.Event
	for i := 0; i < len(calendars); i++ {
		select {
		case err := <-errChan:
			resErr = multierror.Append(resErr, err)
			logrus.Error(err)
		case events := <-resChan:
			res = append(res, events...)
		}
	}

	// return an error if all queries fail, otherwise return the partial result with no error
	if len(res) == 0 && resErr != nil {
		return nil, resErr
	}

	return res, nil
}

// returns a list of all events that are ongoing or happenign in the future, sorted by starting time
func (cal *Calendar) Query(query string, amount int64) ([]*calendar.Event, error) {
	results, err := cal.QueryCalendars(query, func(c *calendar.EventsListCall) { c.MaxResults(amount).TimeMin(time.Now().Format(time.RFC3339)) })
	if err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return StartTime(results[i]).After(StartTime(results[j])) })
	if len(results) > int(amount) {
		return results[:int(amount)], nil
	}
	return results, nil
}

func EndTime(e *calendar.Event) time.Time {
	t, err := toTime(e.End)
	if err != nil {
		logrus.Errorf("event %q (%s) has an invalid end time: %s", e.Summary, e.Id, err)
	}

	return t
}

func StartTime(e *calendar.Event) time.Time {
	t, err := toTime(e.Start)
	if err != nil {
		logrus.Errorf("event %q (%s) has an invalid start time: %s", e.Summary, e.Id, err)
	}

	return t
}

func toTime(calTime *calendar.EventDateTime) (time.Time, error) {
	var t time.Time
	var err error
	if calTime.DateTime != "" {
		t, err = time.Parse(time.RFC3339, calTime.DateTime)
	} else {
		t, err = time.Parse("2006-01-02", calTime.Date)
	}

	return t, err
}

func (cal *Calendar) AddEvent(creator, title, description string, start time.Time, duration time.Duration) error {
	_, err := cal.Events.Insert("primary", &calendar.Event{
		Summary:     title,
		Description: description,
		Creator: &calendar.EventCreator{
			DisplayName: creator,
		},
		Location: "strims.gg",
		Start: &calendar.EventDateTime{
			DateTime: start.Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: start.Add(duration).Format(time.RFC3339),
		},
	}).Do()

	return err
}

func (cal *Calendar) QueryCalendarTitles(query string) (*calendar.Event, error) {
	var earliest *calendar.Event

	for _, id := range cal.CalendarIDsMatching(query) {
		event, err := cal.QueryCalendarSingle("", id, func(c *calendar.EventsListCall) { c.TimeMin(time.Now().Format(time.RFC3339)) })
		if err != nil {
			return nil, fmt.Errorf("failed to fetch first event from calendar: %w", err)
		}
		earliest = FirstEvent(earliest, event)
	}

	return earliest, nil
}

func FirstEvent(events ...*calendar.Event) *calendar.Event {
	var earliestTime time.Time
	var earliest *calendar.Event
	for _, event := range events {
		if event == nil {
			continue
		}
		if earliest == nil {
			earliest = event
			continue
		}

		eventTime := StartTime(event)
		if eventTime.Before(earliestTime) {
			earliestTime = eventTime
			earliest = event
		}
	}

	return earliest
}

func (cal *Calendar) executeListCall(query *calendar.EventsListCall, calID string) ([]*calendar.Event, error) {
	e, err := query.Do()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events for calendar %q: %w", calID, err)
	}
	return e.Items, nil
}

func (cal *Calendar) QueryCalendars(query string, mods ...func(c *calendar.EventsListCall)) ([]*calendar.Event, error) {
	mod := func(c *calendar.EventsListCall) {
		c.ShowDeleted(false).
			SingleEvents(true).
			OrderBy("startTime")
		if query != "" {
			c.Q(query)
		}
	}

	return cal.multiFast(cal.CalendarIDs(), append(mods, mod)...)
}

func (cal *Calendar) QueryCalendarSingle(query, calID string, mods ...func(c *calendar.EventsListCall)) (*calendar.Event, error) {
	q := cal.Events.List(calID).ShowDeleted(false).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(1)

	for _, mod := range mods {
		mod(q)
	}

	events, err := cal.executeListCall(q, calID)
	if err != nil {
		return nil, err
	}
	if len(events) < 1 {
		return nil, nil
	}
	return events[0], nil
}
