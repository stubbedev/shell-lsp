#!/bin/sh
NAME=exampled
PIDFILE=/run/$NAME.pid

start() {
	local pid
	if [ -f "$PIDFILE" ]; then
		read -r pid < "$PIDFILE"
		echo "$NAME already running as $pid"
		return 1
	fi
	/usr/sbin/$NAME --daemon && echo started
}

stop() {
	[ -f "$PIDFILE" ] || return 0
	kill "$(cat "$PIDFILE")" && rm -f "$PIDFILE"
}

case "${1:-}" in
	start) start ;;
	stop) stop ;;
	restart) stop; start ;;
	*) echo "usage: $0 {start|stop|restart}" >&2; exit 64 ;;
esac
