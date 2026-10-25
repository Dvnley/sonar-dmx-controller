.PHONY: run test mac

run:
	go run .

test:
	go vet ./...
	go test ./...

mac:
	./scripts/build-mac.sh
