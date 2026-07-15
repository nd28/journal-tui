BINARY := journal

.PHONY: build run test install clean

build:
	go build -o $(BINARY) ./cmd/journal

run: build
	./$(BINARY)

test:
	go build ./... && go vet ./... && go test ./...

install:
	go install ./cmd/journal

clean:
	rm -f $(BINARY)
