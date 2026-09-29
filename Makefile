# comms-mail builds with every uitoolkit theme engine: the window follows
# the theme chosen for all uitoolkit apps (~/.config/uitoolkit/look.json)
# or its own (Settings › Appearance), and uitoolkit only draws a theme whose
# engine is in the build (its docs/engines.md). The daemon has no window
# and needs none.

GO        ?= go
UI_TAGS   ?= theme_engine_all
BIN       ?= bin

.PHONY: all build install test vet clean

all: build

build:
	$(GO) build -o $(BIN)/comms-maild ./cmd/comms-maild
	$(GO) build -tags '$(UI_TAGS)' -o $(BIN)/comms-mail ./cmd/comms-mail
	$(GO) build -tags '$(UI_TAGS)' -o $(BIN)/comms-mail-demo ./cmd/comms-mail-demo

install:
	$(GO) install ./cmd/comms-maild
	$(GO) install -tags '$(UI_TAGS)' ./cmd/comms-mail ./cmd/comms-mail-demo

test:
	$(GO) test ./...
	$(GO) test -tags '$(UI_TAGS)' ./mailui

vet:
	$(GO) vet ./...
	$(GO) vet -tags '$(UI_TAGS)' ./...

clean:
	rm -rf $(BIN)
