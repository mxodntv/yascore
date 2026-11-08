.PHONY: test integration build tts env-run env-stop run stop up column

# Addresses and paths come from config.yml. The Go program prints them.
CFG_JSON = /tmp/yascore-config.json

test:
	go test ./...

integration:
	go test -tags integration ./autotests/...

build:
	CGO_ENABLED=0 go build -o bin/yascore ./cmd/yascore

bin/yascore:
	CGO_ENABLED=0 go build -o bin/yascore ./cmd/yascore

$(CFG_JSON): bin/yascore config.yml
	./bin/yascore -print > $(CFG_JSON)

tts: $(CFG_JSON)
	scripts/make-tts.sh $(CFG_JSON)

# Dependencies: LAN proxy + Silero TTS.
env-stop:
	systemctl --user stop yascore-proxy.service 2>/dev/null || true
	scripts/make-tts-stop.sh

env-run: env-stop $(CFG_JSON)
	systemctl --user restart yascore-proxy.service
	scripts/make-tts.sh $(CFG_JSON)

# Assistant server.
stop:
	systemctl --user stop yascore.service 2>/dev/null || true

run: stop
	CGO_ENABLED=0 go build -o bin/yascore ./cmd/yascore
	./bin/yascore -print > $(CFG_JSON)
	systemctl --user restart yascore.service
	scripts/make-up.sh $(CFG_JSON)

up: env-run run

column:
	scripts/ytm-player-noproxy.sh
