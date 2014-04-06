DESTDIR =
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

GOBUILDFLAGS =

all: meek-client

meek-client: *.go
	go build $(GOBUILDFLAGS)

clean:
	rm -f meek-client-torbrowser

fmt:
	go fmt

.PHONY: all install clean fmt
