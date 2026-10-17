CC := gcc
CFLAGS = -O2 -Wall
CFLAGS += -g
PREFIX ?= /usr/local
NOW != date
SRCS = main.c \
       util.c
OBJS := $(SRCS:.c=.o)
export TOKEN = abc
override MODE := fast
short::= x

all: app

app: $(OBJS)
	$(CC) $(CFLAGS) -o $@ $^

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

install: app
	install -m 755 app $(PREFIX)/bin

clean:
	rm -f app $(OBJS)

.PHONY: all install clean
