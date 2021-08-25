package chat

import (
	"sort"
	"strings"
)

type UserFeature string

const (
	FeatureMod UserFeature = "moderator"
)

type Message struct {
	Raw       []byte        `json:"-"`
	Sender    string        `json:"nick"`
	Features  []UserFeature `json:"features"`
	Timestamp int64         `json:"timestamp"`
	Data      string        `json:"data"`
	Entities  struct {
		Nicks []struct {
			Nick   string `json:"nick"`
			Bounds []int  `json:"bounds"`
		} `json:"nicks"`
	} `json:"entities"`
}

// Mod returns true if it the message was sent by a mod
func (m Message) Mod() bool {
	for _, f := range m.Features {
		if f == FeatureMod {
			return true
		}
	}

	return false
}

// Addresses returns true if it the message mentions the provided nick
func (m Message) Mentions(nick string) bool {
	for _, n := range m.Entities.Nicks {
		if strings.EqualFold(n.Nick, nick) {
			return true
		}
	}

	return false
}

func (m Message) WithoutNick(nick string) string {
	nicks := m.Entities.Nicks
	// sort descending so we can remove occurrences from right to left
	sort.Slice(nicks, func(i, j int) bool { return nicks[i].Bounds[0] > nicks[j].Bounds[0] })

	patchedMsg := m.Data
	for _, n := range nicks {
		if strings.EqualFold(n.Nick, nick) {
			patchedMsg = patchedMsg[:n.Bounds[0]] + patchedMsg[n.Bounds[1]:]
		}
	}

	return strings.TrimSpace(strings.ReplaceAll(patchedMsg, "  ", ""))
}
