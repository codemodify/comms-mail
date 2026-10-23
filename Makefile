# comms-mail's window is built on goframework.io's desktop-ui. What
# uitoolkit chose with build tags, desktop-ui chooses by import, in
# cmd/comms-mail and cmd/comms-mail-demo (and mailui's tests): every
# window system (desktop-ui-platform-all), every theme engine
# (desktop-ui-themes-all), so the window follows the theme chosen for all
# desktop-ui apps (~/.config/goframework-desktop-ui/look.json) or its own
# (Settings › Appearance), and complex text with colour emoji
# (desktop-ui-text-complex): mail comes in every script, so a rune the
# interface face lacks is borrowed from an installed face, Arabic, Hebrew
# and the Indic scripts are shaped and laid out right to left, lines break
# where Unicode allows, and emoji are drawn in colour. The daemon has no
# window and imports none of it.
#
# Until goframework.io publishes desktop-ui, comms-mail builds against a
# local checkout of the goframework.io workspace, beside this one under
# ~/go/src: go.mod replaces every module of it with its folder (`make
# replace` writes those lines). Build offline (GOPROXY=off), so a module the
# workspace lacks fails instead of coming from the last published release.
#
# The icon packs come from desktop-ui-icons and go beside the binaries, in
# $(BIN)/../share/comms-mail/icons/<set>/, where comms-mail looks for them
# after the user's own icon sets (mailui.UseShippedArt). Nothing is copied
# into ~/.config: that directory is the user's.

GO        ?= go
BIN       ?= bin
SHARE     ?= $(BIN)/../share/comms-mail
ICON_SETS ?= heroicons lucide material-symbols phosphor tabler
ICONS      = goframework.io/desktop-ui-icons
GOFRAMEWORK ?= $(HOME)/go/src/goframework.io

.PHONY: all build icons install test vet clean replace

all: build

build:
	$(GO) build -o $(BIN)/comms-maild ./cmd/comms-maild
	$(GO) build -o $(BIN)/comms-mail ./cmd/comms-mail
	$(GO) build -o $(BIN)/comms-mail-demo ./cmd/comms-mail-demo
	$(MAKE) --no-print-directory icons SHARE='$(SHARE)'

# The module cache is read-only, so the copies are made writable for the
# next build to replace.
icons:
	@src=$$($(GO) list -m -f '{{.Dir}}' $(ICONS)); \
	if [ -z "$$src" ]; then $(GO) mod download $(ICONS) && src=$$($(GO) list -m -f '{{.Dir}}' $(ICONS)); fi; \
	chmod -R u+w '$(SHARE)/icons' 2>/dev/null; rm -rf '$(SHARE)/icons'; \
	mkdir -p '$(SHARE)/icons' && \
	for s in $(ICON_SETS); do cp -R "$$src/$$s" '$(SHARE)/icons/' || exit 1; done && \
	chmod -R u+w '$(SHARE)/icons' && \
	echo "icons: $(ICON_SETS) from $$src -> $(SHARE)/icons"

install:
	$(GO) install ./cmd/comms-maild ./cmd/comms-mail ./cmd/comms-mail-demo
	@gobin=$$($(GO) env GOBIN); [ -n "$$gobin" ] || gobin=$$($(GO) env GOPATH)/bin; \
	$(MAKE) --no-print-directory icons SHARE="$$gobin/../share/comms-mail"

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN)
	chmod -R u+w '$(SHARE)' 2>/dev/null; rm -rf '$(SHARE)'

# replace writes go.mod's replace block: every module of the goframework.io
# workspace at $(GOFRAMEWORK) (desktop-ui-porting.md's list), each by its
# folder, at any version. The modules require one another at versions not
# published (v1.0.0) or no longer matching the folders (v0.0.3), so every
# one is replaced, not only those comms-mail imports. Run it again when the
# workspace gains a module.
replace:
	@W='$(GOFRAMEWORK)'; rel=$$(realpath --relative-to=. "$$W") || exit 1; \
	awk '/^\/\/ goframework.io is not published yet/{skip=1} skip&&/^\)/{skip=0; next} !skip' go.mod | sed -e :a -e '/^\n*$$/{$$d;N;ba' -e '}' > go.mod.new && \
	{ echo; echo '// goframework.io is not published yet: every module comes from the local'; \
	  echo '// checkout of its workspace (make replace writes these lines).'; echo 'replace ('; \
	  (cd "$$W" && for d in */; do d=$${d%/}; [ -f "$$d/go.mod" ] || continue; \
	    case "$$d" in old-*|benchmarks|www-examples|tools|vfs-tests|desktop-ui-tests) continue;; esac; \
	    printf '\t%s => %s/%s\n' "$$(awk '/^module /{print $$2; exit}' "$$d/go.mod")" "$$rel" "$$d"; done); \
	  echo ')'; } >> go.mod.new && mv go.mod.new go.mod && \
	echo "go.mod: $$(grep -c "=> $$rel/" go.mod) modules replaced from $$W"
