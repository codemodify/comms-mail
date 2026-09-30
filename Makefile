# comms-mail builds with every uitoolkit theme engine: the window follows
# the theme chosen for all uitoolkit apps (~/.config/uitoolkit/look.json)
# or its own (Settings › Appearance), and uitoolkit only draws a theme whose
# engine is in the build (its docs/engines.md). The daemon has no window
# and needs none.
#
# The icon packs come from the uitoolkit the window app is built with and go
# beside the binaries, in $(BIN)/../share/comms-mail/icons/<set>/, where
# comms-mail looks for them after the user's own ~/.config/uitoolkit/icons
# (mailui.UseShippedArt). Nothing is copied into ~/.config: that directory
# is the user's.

GO        ?= go
UI_TAGS   ?= theme_engine_all
BIN       ?= bin
SHARE     ?= $(BIN)/../share/comms-mail
ICON_SETS ?= heroicons lucide material-symbols phosphor tabler
UITK       = github.com/codemodify/uitoolkit

.PHONY: all build icons install test vet clean

all: build

build:
	$(GO) build -o $(BIN)/comms-maild ./cmd/comms-maild
	$(GO) build -tags '$(UI_TAGS)' -o $(BIN)/comms-mail ./cmd/comms-mail
	$(GO) build -tags '$(UI_TAGS)' -o $(BIN)/comms-mail-demo ./cmd/comms-mail-demo
	$(MAKE) --no-print-directory icons SHARE='$(SHARE)'

# The module cache is read-only, so the copies are made writable for the
# next build to replace.
icons:
	@src=$$($(GO) list -m -f '{{.Dir}}' $(UITK)); \
	if [ -z "$$src" ]; then $(GO) mod download $(UITK) && src=$$($(GO) list -m -f '{{.Dir}}' $(UITK)); fi; \
	chmod -R u+w '$(SHARE)/icons' 2>/dev/null; rm -rf '$(SHARE)/icons'; \
	mkdir -p '$(SHARE)/icons' && \
	for s in $(ICON_SETS); do cp -R "$$src/icons/$$s" '$(SHARE)/icons/' || exit 1; done && \
	chmod -R u+w '$(SHARE)/icons' && \
	echo "icons: $(ICON_SETS) from $$src -> $(SHARE)/icons"

install:
	$(GO) install ./cmd/comms-maild
	$(GO) install -tags '$(UI_TAGS)' ./cmd/comms-mail ./cmd/comms-mail-demo
	@gobin=$$($(GO) env GOBIN); [ -n "$$gobin" ] || gobin=$$($(GO) env GOPATH)/bin; \
	$(MAKE) --no-print-directory icons SHARE="$$gobin/../share/comms-mail"

test:
	$(GO) test ./...
	$(GO) test -tags '$(UI_TAGS)' ./mailui

vet:
	$(GO) vet ./...
	$(GO) vet -tags '$(UI_TAGS)' ./...

clean:
	rm -rf $(BIN)
	chmod -R u+w '$(SHARE)' 2>/dev/null; rm -rf '$(SHARE)'
