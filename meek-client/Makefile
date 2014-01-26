DESTDIR =
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

GOBUILDFLAGS =

all: meek-client

meek-client: *.go
	go build $(GOBUILDFLAGS)

install: meek-client
	mkdir -p "$(DESTDIR)$(BINDIR)"
	cp -f meek-client "$(DESTDIR)$(BINDIR)"

clean:
	rm -f meek-client

fmt:
	go fmt

.PHONY: all install clean fmt
