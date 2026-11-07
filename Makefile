.PHONY: test integration build tts up column

# Addresses and paths come from config.yml. The Go program prints them.
CFG_JSON = /tmp/yascore-config.json

test:
	go test ./...

integration:
	go test -tags integration ./autotests/...

build: bin/yascore

bin/yascore:
	CGO_ENABLED=0 go build -o bin/yascore ./cmd/yascore

$(CFG_JSON): bin/yascore config.yml
	./bin/yascore -print > $(CFG_JSON)

tts: $(CFG_JSON)
	scripts/make-tts.sh $(CFG_JSON)

up: tts
	systemctl --user restart yascore.service
	scripts/make-up.sh $(CFG_JSON)

column:
	scripts/ytm-player-noproxy.sh
