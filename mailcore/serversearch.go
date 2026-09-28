package mailcore

import (
	"fmt"
	"strings"
	"unicode"
)

// Searching the server. Every message's headers are in the cache, but only
// some bodies are (the recent ones, and any opened); the local search sees
// what is cached. The server has every body, so asking it — IMAP UID SEARCH
// TEXT, per folder — finds the words in mail not yet downloaded, and each
// UID it answers names a message the cache already lists.

// maxServerSearchFolders bounds one search over every folder.
const maxServerSearchFolders = 200

// searchTerms splits a query into the words a message must all contain,
// keeping "quoted phrases" together.
func searchTerms(q string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for _, r := range q {
		switch {
		case r == '"':
			flush()
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		case unicode.IsControl(r):
			// never part of a word, and never into a command
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] < 0x20 {
			return false
		}
	}
	return true
}

// SearchServer asks the server which messages in folder — or, with folder
// empty, in every folder of every IMAP account — contain every word of
// query, and returns those the cache has. A folder the server cannot search
// is skipped; the first error is returned with what was found.
func (s *LocalStore) SearchServer(folder FolderID, query string) ([]Message, error) {
	query = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, query)
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	var folders []Folder
	for _, f := range s.Folders {
		if f.Virtual || f.AccountID == LocalAccountID || f.Remote == "" {
			continue
		}
		if folder != "" && f.ID != folder {
			continue
		}
		if cfg, ok := s.accountCfgLocked(f.AccountID); !ok || cfg.IsLocal() || cfg.IsPOP3() {
			continue
		}
		folders = append(folders, f)
	}
	s.mu.Unlock()
	if len(folders) > maxServerSearchFolders {
		folders = folders[:maxServerSearchFolders]
	}
	if s.feat != nil && !s.feat.Online() {
		return nil, fmt.Errorf("mail: working offline — the server cannot be searched")
	}

	var out []Message
	var firstErr error
	for _, f := range folders {
		uids, err := s.searchFolderOnServer(f, terms, strings.TrimSpace(query))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		s.mu.Lock()
		for _, u := range uids {
			if i, ok := s.indexLocked(MessageID(fmt.Sprintf("%s:%d", f.ID, u))); ok {
				out = append(out, s.Messages[i].Clone())
			}
		}
		s.mu.Unlock()
	}
	return out, firstErr
}

// searchFolderOnServer runs one UID SEARCH in f. On Gmail the query goes
// whole in its own syntax (X-GM-RAW). Elsewhere ASCII words go as quoted
// strings, TEXT each; a query with other characters goes as one UTF-8
// literal (quoted strings are 7-bit), the words as a phrase.
func (s *LocalStore) searchFolderOnServer(f Folder, terms []string, raw string) ([]uint32, error) {
	cli, err := s.client(f.AccountID)
	if err != nil {
		return nil, err
	}
	ascii := true
	for _, t := range terms {
		ascii = ascii && isASCII(t)
	}
	var uids []uint32
	err = cli.inBox(func() error {
		if _, err := cli.selectBox(remoteName(f), true); err != nil {
			return err
		}
		if cli.has("X-GM-EXT-1") {
			// Gmail: its own search language, as in its web search box
			// (has:attachment, older_than:1y, from:, "phrases"…).
			if isASCII(raw) {
				uids, err = cli.search("X-GM-RAW " + imapQuote(raw))
			} else {
				uids, err = cli.searchLiteral("CHARSET UTF-8 X-GM-RAW", raw)
			}
			return err
		}
		if ascii {
			parts := make([]string, len(terms))
			for i, t := range terms {
				parts[i] = "TEXT " + imapQuote(t)
			}
			uids, err = cli.search(strings.Join(parts, " "))
			return err
		}
		uids, err = cli.searchLiteral("CHARSET UTF-8 TEXT", strings.Join(terms, " "))
		return err
	})
	return uids, err
}
