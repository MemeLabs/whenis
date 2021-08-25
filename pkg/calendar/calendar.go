package calendar

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/MemeLabs/whenis/pkg/util"
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
	var candidates []*calendar.Event
	for _, calId := range cal.CalendarIDs() {
		events, err := cal.QueryCalendar("", calId, func(c *calendar.EventsListCall) { c.TimeMax(endTime).TimeMin(startTime) })
		if err != nil {
			return nil, fmt.Errorf("failed to get ongoing events for cal %s: %w", calId, err)
		}
		candidates = append(candidates, events...)
	}
	var results []*calendar.Event
	for _, event := range candidates {
		if EndTime(event).After(time.Now()) {
			results = append(results, event)
		}
	}

	return results, nil
}

// returns a list of all events that are ongoing or happenign in the future, sorted by starting time
func (cal *Calendar) Query(query string, amount int64) ([]*calendar.Event, error) {
	var results []*calendar.Event
	for _, item := range cal.CalendarIDs() {
		events, err := cal.QueryCalendar(query, item, func(c *calendar.EventsListCall) { c.MaxResults(amount) })
		if err != nil {
			return nil, err
		}
		results = append(results, events...)
	}

	sort.Slice(results, func(i, j int) bool { return StartTime(results[i]).Before(StartTime(results[j])) })
	if len(results) > int(amount) {
		return results[:int(amount)], nil
	}
	return results, nil
}

func EndTime(e *calendar.Event) time.Time {
	t, err := time.Parse(time.RFC3339, e.End.DateTime)
	if err != nil {
		logrus.Errorf("event %q has an invalid end time: %s", e.Id, err)
	}

	return t
}

func StartTime(e *calendar.Event) time.Time {
	t, err := time.Parse(time.RFC3339, e.End.DateTime)
	if err != nil {
		logrus.Errorf("event %q has an invalid start time: %s", e.Id, err)
	}

	return t
}

func (cal *Calendar) QueryCalendarTitles(query string) (*calendar.Event, error) {
	var earliest *calendar.Event
	cal.RLock()
	defer cal.RUnlock()
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

func (cal *Calendar) QueryCalendar(query, calID string, mods ...func(c *calendar.EventsListCall)) ([]*calendar.Event, error) {
	req := cal.Events.
		List(calID).
		ShowDeleted(false).
		SingleEvents(true).
		OrderBy("startTime")

	for _, mod := range mods {
		mod(req)
	}

	if query != "" {
		req = req.Q(query)
	}

	e, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events for calendar %q: %w", calID, err)
	}
	return e.Items, nil
}

func (cal *Calendar) QueryCalendarSingle(query, calID string, mods ...func(c *calendar.EventsListCall)) (*calendar.Event, error) {
	events, err := cal.QueryCalendar(query, calID, append(mods, func(c *calendar.EventsListCall) { c.MaxResults(1) })...)
	if err != nil {
		return nil, err
	}
	if len(events) < 1 {
		return nil, nil
	}
	return events[0], nil
}
