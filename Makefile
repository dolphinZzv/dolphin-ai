VERSION ?= dev
APP_BUNDLE := panda.app
PANDA_DIR := app/panda

.PHONY: build run clean test fmt check-fmt init-hooks llm-smoke docs-smoke app app-clean distribute

build:
	go build -ldflags="-X 'dolphin/cmd.Version=$(VERSION)'" -o dolphin .

run: build
	./dolphin

clean:
	rm -f dolphin
	rm -f /tmp/dolphin/*.jsonl

check-fmt:
	@gofmt -l . | grep -q . && { \
		echo "Unformatted files:"; \
		gofmt -l .; \
		exit 1; \
	} || exit 0

test: check-fmt
	go test ./...

fmt:
	gofmt -w .

init-hooks:
	@hooks=".git/hooks"; \
	mkdir -p "$$hooks"; \
	printf '#!/bin/bash\nset -euo pipefail\nfiles=$$(git diff --cached --name-only --diff-filter=ACM | grep "\\.go$$" || true)\n[ -z "$$files" ] && exit 0\ngofmt -w $$files\necho "$$files" | tr " " "\\n" | xargs git add\n' > "$$hooks/pre-commit"; \
	chmod +x "$$hooks/pre-commit"; \
	echo "pre-commit hook installed (runs gofmt on staged .go files)"

llm-smoke:
	@scripts/llm-smoke.sh

docs-smoke:
	@scripts/docs-smoke.sh

app:
	$(MAKE) -C $(PANDA_DIR) build VERSION=$(VERSION)
	cp -r $(PANDA_DIR)/$(APP_BUNDLE) .

app-clean:
	rm -rf $(APP_BUNDLE)

# Install to systemd (Linux)
install-systemd: build
	mkdir -p /usr/local/bin /etc/dolphin /var/lib/dolphin/.dolphin/logs
	cp dolphin /usr/local/bin/dolphin
	cp deploy/dolphin.service /etc/systemd/system/dolphin.service
	sudo systemctl daemon-reload
	@echo "dolphin installed. Edit /etc/dolphin/config.yaml, then:"
	@echo "  sudo systemctl enable --now dolphin"
	@echo "  journalctl -u dolphin -f"

release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v0.1.0"; exit 1; fi
	git tag -a "$(TAG)" -m "release $(TAG)"
	git push origin "$(TAG)"

release-snapshot:
	goreleaser release --snapshot --clean

distribute:
	@branch=$$(git symbolic-ref --short HEAD); \
	echo "Pushing $$branch to github, gitee..."; \
	git push github "$$branch" && echo "  ✓ github" \
		|| echo "  ✗ github"; \
	git push gitee "$$branch" && echo "  ✓ gitee" \
		|| echo "  ✗ gitee"; \
	echo "Done."
