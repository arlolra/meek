MAN = meek-client.1 meek-server.1

all: $(MAN)

%.1: %.1.txt
	a2x --no-xmllint --xsltproc-opts "--stringparam man.th.title.max.length 12" -d manpage -f manpage $<

clean:
	rm -f $(MAN)

.PHONY: all clean
