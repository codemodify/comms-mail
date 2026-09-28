// Command comms-maild is the comms-mail daemon: accounts, folders and
// messages, IMAP/POP3 + SMTP + OAuth, with an on-disk cache. No config means
// an empty store (first run offers add-account). The seeded MemoryStore is
// explicit UITK_MAIL=memory. It listens on a Unix socket and speaks JSON-RPC
// 2.0 (NDJSON). See docs/mail.md.
//
//	comms-maild
//	comms-maild install     # start at every login (systemd user unit, or XDG autostart)
//	comms-maild uninstall   # stop starting at login
//	UITK_MAIL=memory comms-maild   # seeded demo store
//	# real account: File -> Add Account in the UI, or ~/.config/uitoolkit/mail.json
//	UITK_MAIL_PASS=secret comms-maild
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/codemodify/comms-mail/mailcore"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(command(os.Args[1]))
	}
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
	if store.Backend() != "memory" {
		if p, err := mailcore.OpenLog(filepath.Join(mailcore.DataDir(), "logs")); err == nil {
			fmt.Println("log:", p)
			mailcore.Logf("comms-maild started (pid %d, backend %s, socket %s)", os.Getpid(), store.Backend(), sock)
			defer mailcore.Logf("comms-maild stopped")
		}
	}
	if err := mailcore.ListenAndServe(ctx, sock, store); err != nil {
		// A lock-file clash means another comms-maild already owns the
		// socket; saying so beats a silent takeover. Exit 3 tells systemd
		// not to restart into the same clash.
		mailcore.Logf("comms-maild: %v", err)
		fmt.Fprintf(os.Stderr, "comms-maild: %v\n", err)
		if errors.Is(err, mailcore.ErrDaemonRunning) {
			os.Exit(3)
		}
		os.Exit(1)
	}
}

// command runs `comms-maild install|uninstall|help` and returns the exit
// status.
func command(name string) int {
	switch name {
	case "install":
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "comms-maild:", err)
			return 1
		}
		msg, err := mailcore.InstallService(exe)
		if err != nil {
			fmt.Fprintln(os.Stderr, "comms-maild install:", err)
			return 1
		}
		fmt.Println(msg)
	case "uninstall":
		msg, err := mailcore.UninstallService()
		if err != nil {
			fmt.Fprintln(os.Stderr, "comms-maild uninstall:", err)
			return 1
		}
		fmt.Println(msg)
	case "help", "-h", "-help", "--help":
		fmt.Println("comms-maild            run the daemon (the window starts it when needed)")
		fmt.Println("comms-maild install   start it at every login")
		fmt.Println("comms-maild uninstall stop starting it at login")
	default:
		fmt.Fprintf(os.Stderr, "comms-maild: unknown command %q (try: install, uninstall)\n", name)
		return 2
	}
	return 0
}
