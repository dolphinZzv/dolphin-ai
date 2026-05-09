VERSION ?= dev
APP_DIR := app/dolphin
APP_BUNDLE := dolphin.app

.PHONY: build run clean test fmt check-fmt init-hooks app app-clean

build:
	go build -ldflags="-X 'dolphinzZ/cmd.Version=$(VERSION)'" -o dolphinzZ .

run: build
	./dolphinzZ

clean:
	rm -f dolphinzZ
	rm -f /tmp/dolphinzZ/*.jsonl

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

app:
	swift build --package-path $(APP_DIR)
	rm -rf $(APP_BUNDLE)
	mkdir -p $(APP_BUNDLE)/Contents/MacOS
	cp $(APP_DIR)/.build/debug/dolphin $(APP_BUNDLE)/Contents/MacOS/
	printf '<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0">\n<dict>\n<key>CFBundleExecutable</key>\n<string>dolphin</string>\n<key>CFBundleIdentifier</key>\n<string>space.siciv.dolphinzZ</string>\n<key>CFBundleName</key>\n<string>dolphin</string>\n<key>CFBundlePackageType</key>\n<string>APPL</string>\n<key>CFBundleShortVersionString</key>\n<string>$(VERSION)</string>\n<key>LSUIElement</key>\n<false/>\n</dict>\n</plist>\n' > $(APP_BUNDLE)/Contents/Info.plist
		open $(APP_BUNDLE)

app-clean:
	rm -rf $(APP_BUNDLE)

release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v0.1.0"; exit 1; fi
	git tag -a "$(TAG)" -m "release $(TAG)"
	git push origin "$(TAG)"

release-snapshot:
	goreleaser release --snapshot --clean
