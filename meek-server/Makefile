DESTDIR =
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

GOBUILDFLAGS =

all: meek-server

meek-server: *.go
	go build $(GOBUILDFLAGS)

install: meek-server
	mkdir -p "$(DESTDIR)$(BINDIR)"
	cp -f meek-server "$(DESTDIR)$(BINDIR)"

clean:
	rm -f meek-server

fmt:
	go fmt

.PHONY: all install clean fmt
