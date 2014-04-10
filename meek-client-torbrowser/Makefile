DESTDIR =
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

GOBUILDFLAGS =

all: meek-client-torbrowser

meek-client-torbrowser: *.go
	go build $(GOBUILDFLAGS)

clean:
	rm -f meek-client-torbrowser

fmt:
	go fmt

.PHONY: all install clean fmt
