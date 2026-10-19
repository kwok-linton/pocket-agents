.PHONY: build check frontend test clean cross licenses release deploy-ui
build: frontend
	mkdir -p bin
	go build -trimpath -o bin/pocket ./cmd/pocket
frontend:
	cd web && npm ci && npm run build
	rm -rf internal/web/dist
	cp -R web/dist internal/web/dist
check:
	go vet ./...
	go test -race ./...
	cd web && npm run build && npm test
test:
	go test ./...
cross: frontend
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o bin/pocket-darwin-arm64 ./cmd/pocket
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/pocket-linux-amd64 ./cmd/pocket
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/pocket-linux-arm64 ./cmd/pocket
clean:
	rm -rf bin internal/web/dist web/dist
licenses:
	python3 scripts/licenses.py
release: cross licenses
	python3 scripts/package-release.py

# UI-only releases preserve the installed executable's macOS privacy identity.
deploy-ui:
	cd web && npm ci && npm run build
	$(HOME)/.local/bin/pocket ui install --from "$(CURDIR)/web/dist"

# Stable development signing; never regenerate the per-user identity on updates.
sign-local:
	python3 scripts/sign-macos-local.py bin/pocket

deploy-local: build sign-local
	./bin/pocket service install --role both
	$(HOME)/.local/bin/pocket service restart
	$(HOME)/.local/bin/pocket ui install --from "$(CURDIR)/web/dist"
