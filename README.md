## running whenis

You will need googleconfig.json, get it from here https://developers.google.com/calendar/quickstart/go
when you first start whenis it will prompt ask you for authorizartion.

whenis will search all calendars from the connected account for events. Both even title and description are searched. If no events are found it will also search calendar titles.

## commands

You can interact with whenis using following commands 

`/msg whenis -help` to display this info  
`/msg whenis Formula 1` to search for an event (in this case F1)  
`/msg whenis -multi 5` Formula 1 to search for the next 5 F1 events  
`/msg whenis -next` to show the next scheduled event  
`/msg whenis -ongoing` to show a list of all ongoing events  
`/msg whenis -start` 20 Session Title adds a session to the calendar with a duration of 20 minutes and the title 'Session Title'   (abusing this will get you blacklisted)  
`/msg whenis -calendars` to get a list of active calendars  

All of these also work in public chat, but some will only reply with private messages
