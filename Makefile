BINARY := cormake

.PHONY: build clean

build:
	go build -o $(BINARY) ./cmd/cormake

clean:
	rm -f $(BINARY)
