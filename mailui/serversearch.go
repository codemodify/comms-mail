package mailui

import (
	"fmt"
	"strings"
	"time"

	"github.com/codemodify/comms-mail/mailcore"
)

// "On the server too", in the search dialog, asks the mail server as well.
// The local search covers every message's headers and the bodies already
// downloaded; the server has every body. With it on, a search is sent to
// the server as well (this folder, or every folder with All folders), and
// what it finds is merged into the list — marked in the status line —
// without waiting on it: the local results show at once.

// serverSearchDelay is how long a query rests before the server is asked,
// so typing a word does not send a search per letter.
var serverSearchDelay = 700 * time.Millisecond

// serverSearch is the setting's state (the search dialog's "On the
// server too") and the latest answer.
type serverSearch struct {
	on    bool
	key   string // the scope and query hits answers
	hits  []mailcore.Message
	asked string // the scope and query last sent
	timer *time.Timer
	gen   uint64
}

// serverScope is what a server search is for: the folder (or all) and the
// query.
func (s *session) serverScope() (folder mailcore.FolderID, query, key string) {
	query = strings.TrimSpace(s.filter.Query)
	if !s.searchAll {
		folder = s.folder
	}
	return folder, query, string(folder) + "\x00" + query
}

// maybeSearchServer schedules a server search for the current query when
// it differs from the last one asked.
func (s *session) maybeSearchServer() {
	folder, query, key := s.serverScope()
	if !s.srv.on || query == "" || mailcore.IsVirtual(folder) || key == s.srv.asked {
		return
	}
	s.srv.asked = key
	s.srv.gen++
	gen := s.srv.gen
	run := func() {
		if gen != s.srv.gen {
			return // a newer query took its place
		}
		s.mark("Searching the server for “" + query + "”…")
		s.async(func() (any, error) {
			return s.cli.SearchServer(folder, query)
		}, func(v any, err error) {
			if gen != s.srv.gen {
				return
			}
			if err != nil {
				s.mark("Server search: " + err.Error())
				return
			}
			s.srv.key, s.srv.hits = key, v.([]mailcore.Message)
			s.refreshList()
			switch s.srvAdded {
			case 0:
				s.mark("The server found nothing more")
			case 1:
				s.mark("The server found 1 more message")
			default:
				s.mark(fmt.Sprintf("The server found %d more messages", s.srvAdded))
			}
		})
	}
	if s.srv.timer != nil {
		s.srv.timer.Stop()
	}
	if !appLooping(s.app) {
		run() // headless: no loop to come back on
		return
	}
	s.srv.timer = time.AfterFunc(serverSearchDelay, func() { s.post(run) })
}

// mergeServerHits adds the server's answer for the current query to all,
// once each and only if it passes the list's other filters (unread,
// starred, …); it reports how many it added.
func (s *session) mergeServerHits(all []mailcore.Message) ([]mailcore.Message, int) {
	_, _, key := s.serverScope()
	if !s.srv.on || key != s.srv.key || len(s.srv.hits) == 0 {
		return all, 0
	}
	have := make(map[mailcore.MessageID]bool, len(all))
	for _, m := range all {
		have[m.ID] = true
	}
	pins := s.filter
	pins.Query = "" // the server matched the words
	added := 0
	for _, m := range s.srv.hits {
		if have[m.ID] || s.hidden[m.ID] || !pins.Match(m) {
			continue
		}
		have[m.ID] = true
		all = append(all, m)
		added++
	}
	return all, added
}
