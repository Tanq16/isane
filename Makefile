.PHONY: help assets font build docker run clean

APP_NAME := isane

VERSION ?= dev-build

MARKED_VERSION      := 18.0.11
MERMAID_VERSION     := 11.17.2
HIGHLIGHTJS_VERSION := 11.12.0
LIVEKIT_JS_VERSION  := 2.22.1
TAILWIND_VERSION    := 4.3.3

WEB_DIR    := web
CSS_DIR    := $(WEB_DIR)/css
VENDOR_DIR := $(WEB_DIR)/vendor
FONTS_DIR  := $(VENDOR_DIR)/fonts
STAMP      := $(VENDOR_DIR)/.stamp

TAILWIND_BIN   := dist/tailwindcss
TAILWIND_OS    := $(shell uname -s | tr '[:upper:]' '[:lower:]' | sed 's/darwin/macos/')
TAILWIND_ARCH  := $(shell uname -m | sed 's/aarch64/arm64/;s/x86_64/x64/')
TAILWIND_LIBC  := $(shell test -f /etc/alpine-release && echo -musl)
TAILWIND_ASSET := tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)$(TAILWIND_LIBC)
TAILWIND_BASE  := https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)

# Google Fonts serves woff2 only to a browser-shaped User-Agent; anything else gets ttf.
UA := Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36

CYAN  := \033[0;36m
GREEN := \033[0;32m
NC    := \033[0m

help: ## Show this help
	@echo "$(CYAN)Available targets:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

.DEFAULT_GOAL := help

define npm_file
@set -e; tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
meta="$$(curl -sfL 'https://registry.npmjs.org/$(1)/$(2)')"; \
url="$$(printf '%s' "$$meta" | jq -er '.dist.tarball')"; \
want="$$(printf '%s' "$$meta" | jq -er '.dist.integrity' | sed 's/^sha512-//')"; \
curl -sfL "$$url" -o "$$tmp/pkg.tgz"; \
got="$$(openssl dgst -sha512 -binary "$$tmp/pkg.tgz" | openssl base64 -A)"; \
test "$$want" = "$$got" || { echo "checksum mismatch: $(1)@$(2)"; exit 1; }; \
tar -xzf "$$tmp/pkg.tgz" -C "$$tmp" "package/$(3)"; \
cp "$$tmp/package/$(3)" "$(4)"
endef

assets: $(STAMP) $(CSS_DIR)/app.css ## Vendor the pinned frontend assets and compile the stylesheet
	@:

# Every pin lives in this file, so bumping one invalidates the stamp and re-downloads.
$(STAMP): $(MAKEFILE_LIST)
	@mkdir -p $(VENDOR_DIR) $(FONTS_DIR)
	$(call npm_file,marked,$(MARKED_VERSION),lib/marked.umd.js,$(VENDOR_DIR)/marked.min.js)
	$(call npm_file,mermaid,$(MERMAID_VERSION),dist/mermaid.min.js,$(VENDOR_DIR)/mermaid.min.js)
	$(call npm_file,@highlightjs/cdn-assets,$(HIGHLIGHTJS_VERSION),highlight.min.js,$(VENDOR_DIR)/highlight.min.js)
	$(call npm_file,@highlightjs/cdn-assets,$(HIGHLIGHTJS_VERSION),styles/github-dark.min.css,$(VENDOR_DIR)/highlight.css)
	$(call npm_file,livekit-client,$(LIVEKIT_JS_VERSION),dist/livekit-client.umd.js,$(VENDOR_DIR)/livekit-client.umd.min.js)
	@for w in 400 500 600 700; do $(MAKE) --no-print-directory font FAMILY=Inter SLUG=inter WEIGHT=$$w; done
	@for w in 400 600; do $(MAKE) --no-print-directory font FAMILY=JetBrains+Mono SLUG=jetbrains-mono WEIGHT=$$w; done
	@touch $(STAMP)
	@echo "$(GREEN)Vendored $(VENDOR_DIR)$(NC)"

# The endpoint declares every subset the family has, so only the latin block is taken.
font:
	@set -e; css="$$(curl -sfL -H "User-Agent: $(UA)" \
	  "https://fonts.googleapis.com/css2?family=$(FAMILY):wght@$(WEIGHT)&display=swap")"; \
	url="$$(printf '%s\n' "$$css" | awk '/^\/\* latin \*\/$$/{f=1} f && /src:/{print; exit}' | grep -o 'https://fonts.gstatic.com/[^)]*')"; \
	test -n "$$url" || { echo "no latin woff2 for $(FAMILY) $(WEIGHT)"; exit 1; }; \
	curl -sfL "$$url" -o "$(FONTS_DIR)/$(SLUG)-$(WEIGHT).woff2"

$(TAILWIND_BIN): $(MAKEFILE_LIST)
	@mkdir -p $(dir $(TAILWIND_BIN))
	@set -e; curl -sfL "$(TAILWIND_BASE)/$(TAILWIND_ASSET)" -o "$(TAILWIND_BIN)"; \
	want="$$(curl -sfL "$(TAILWIND_BASE)/sha256sums.txt" | awk '$$2 == "./$(TAILWIND_ASSET)" {print $$1}')"; \
	got="$$(openssl dgst -sha256 "$(TAILWIND_BIN)" | awk '{print $$NF}')"; \
	test -n "$$want" && test "$$want" = "$$got" || { echo "checksum mismatch: $(TAILWIND_ASSET)"; exit 1; }; \
	chmod +x "$(TAILWIND_BIN)"

$(CSS_DIR)/app.css: $(CSS_DIR)/input.css $(TAILWIND_BIN)
	@$(TAILWIND_BIN) -i $(CSS_DIR)/input.css -o $@ --minify
	@echo "$(GREEN)Built: $@$(NC)"

build: assets ## Build the binary for this machine
	@CGO_ENABLED=0 go build -ldflags="-s -w -X 'main.AppVersion=$(VERSION)'" -o $(APP_NAME) ./cmd/$(APP_NAME)
	@echo "$(GREEN)Built: ./$(APP_NAME)$(NC)"

docker: ## Build the container image
	@docker build --build-arg VERSION=$(VERSION) -t $(APP_NAME):$(VERSION) -t $(APP_NAME):latest .

run: assets ## Run against http://localhost:8080 with debug logging
	@go run ./cmd/$(APP_NAME) --debug

clean: ## Remove the binary, the vendored assets, and the compiled stylesheet
	@rm -f $(APP_NAME) $(CSS_DIR)/app.css
	@rm -rf $(VENDOR_DIR) dist
	@echo "$(GREEN)Cleaned$(NC)"
