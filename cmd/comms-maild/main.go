// Command comms-maild is the comms-mail daemon: accounts, folders and
// messages, IMAP/POP3 + SMTP + OAuth, with an on-disk cache. No config means
// an empty store (first run offers add-account). The seeded MemoryStore is
// explicit UITK_MAIL=memory. It listens on a Unix socket and speaks JSON-RPC
// 2.0 (NDJSON). See docs/mail.md.
//
//	comms-maild
//	UITK_MAIL=memory comms-maild   # seeded demo store
//	# real account: File -> Add Account in the UI, or ~/.config/uitoolkit/mail.json
//	UITK_MAIL_PASS=secret comms-maild
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/codemodify/comms-mail/mailcore"
)

func main() {
	sock := mailcore.DefaultSocket()
	store, err := mailcore.OpenStore()
	if err != nil && store == nil {
		log.Fatal(err)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "comms-maild: %v (serving anyway; Health reports the error)\n", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("comms-maild  backend=%s  socket=%s\n", store.Backend(), sock)
	fmt.Println("JSON-RPC 2.0 NDJSON (socket is mode 0600, same-uid only). Docs: docs/mail.md")
	if err := mailcore.ListenAndServe(ctx, sock, store); err != nil {
		// A lock-file clash means another comms-maild already owns the
		// socket; saying so beats a silent takeover.
		log.Fatalf("comms-maild: %v", err)
	}
}
