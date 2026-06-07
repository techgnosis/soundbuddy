BINARY := soundbuddy
PKG    := ./cmd/soundbuddy

.PHONY: build run test clean

build:
	go build -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)
