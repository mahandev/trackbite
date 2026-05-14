# trackbite — build/run targets.
# Run `make help` to see what each target does.

.DEFAULT_GOAL := help

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n",$$1,$$2}'

helper: ## Build the Swift trackpad helper (trackweighd).
	cd trackweighd && swift build -c release
	mkdir -p trackweighd/bin
	cp trackweighd/.build/release/trackweighd trackweighd/bin/trackweighd
	@echo "✓ Built trackweighd/bin/trackweighd"

app: ## Build the Go application.
	go build -o trackbite .
	@echo "✓ Built ./trackbite"

build: helper app ## Build everything.

run: build ## Build and run.
	./trackbite

deps: ## Install macOS deps (Homebrew + opencv + pkg-config).
	@which brew >/dev/null || { echo "Install Homebrew first: https://brew.sh"; exit 1; }
	brew install opencv pkg-config
	@echo "✓ System dependencies installed"

clean: ## Remove build artifacts.
	rm -rf trackbite trackweighd/.build trackweighd/bin trackbite.db

.PHONY: help helper app build run deps clean
