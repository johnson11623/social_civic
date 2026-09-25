#!/usr/bin/env bash
# Local development in one command (make dev / dev-stop / dev-status / dev-logs).
#
# Starts the infrastructure in Docker, then the API, the outbox relay, the
# media worker and the web app as background processes with logs in
# .dev/logs. Waits until each is healthy and opens the web app.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEV="$ROOT/.dev"
LOGS="$DEV/logs"
PIDS="$DEV/pids"
WEB_URL="http://localhost:3000"
API_URL="http://localhost:8090"
mkdir -p "$LOGS" "$PIDS"
cd "$ROOT"

say() { printf '\033[1;32m▸\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m✗\033[0m %s\n' "$*"; exit 1; }

# Wait until a command succeeds, up to $2 seconds.
wait_for() {
	local what=$1 secs=$2; shift 2
	for _ in $(seq 1 "$secs"); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	return 1
}

running() { [ -f "$PIDS/$1" ] && kill -0 "$(cat "$PIDS/$1")" 2>/dev/null; }

# Start a named background process in its own process group.
start() {
	local name=$1; shift
	if running "$name"; then say "$name already running"; return; fi
	: >"$LOGS/$name.log"
	# setsid is not on macOS; perl's setpgrp gives the process its own group.
	perl -e 'setpgrp(0,0); exec @ARGV' bash -c "$*" >>"$LOGS/$name.log" 2>&1 &
	echo $! >"$PIDS/$name"
	say "$name started (logs: .dev/logs/$name.log)"
}

stop_one() {
	local name=$1
	if running "$name"; then
		local pid; pid=$(cat "$PIDS/$name")
		kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
		for _ in $(seq 1 10); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
		kill -KILL -- "-$pid" 2>/dev/null || true
		say "$name stopped"
	fi
	rm -f "$PIDS/$name"
}

port_owner() { lsof -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -1; }

up() {
	command -v docker >/dev/null || fail "Docker is required."
	docker info >/dev/null 2>&1 || fail "Docker isn't running. Start Docker Desktop and try again."
	command -v go >/dev/null || fail "Go is required."
	command -v pnpm >/dev/null || fail "pnpm is required (corepack enable)."

	# A port is fine if our own service holds it (make dev run twice).
	for entry in api:8090 web:3000; do
		name=${entry%%:*} p=${entry##*:}
		running "$name" && continue
		owner=$(port_owner "$p" || true)
		if [ -n "$owner" ]; then
			warn "port $p is used by another process (pid $owner): $(ps -o comm= -p "$owner")"
			fail "Stop it first (kill $owner), then run make dev again."
		fi
	done

	say "Starting infrastructure (Postgres, Redis, Redpanda, SeaweedFS, Caddy)…"
	docker compose up -d --wait postgres redis redpanda >/dev/null
	docker compose --profile media up -d --wait seaweedfs caddy >/dev/null
	say "Applying migrations and reference data…"
	make -s migrate-up >/dev/null
	make -s db-seed >/dev/null

	if [ ! -d civic-console/node_modules ]; then
		say "Installing web dependencies…"
		(cd civic-console && pnpm install --frozen-lockfile >/dev/null)
	fi

	say "Building Go services…"
	go build -o "$DEV/bin/api" ./cmd/api
	go build -o "$DEV/bin/worker" ./cmd/worker
	go build -o "$DEV/bin/mediaworker" ./cmd/mediaworker

	start api "env $API_ENV '$DEV/bin/api'"
	start relay "env DATABASE_URL='$HOST_DB_URL' KAFKA_BROKERS='localhost:$KAFKA_PORT' '$DEV/bin/worker'"
	if command -v ffmpeg >/dev/null; then
		start media "env $MEDIA_WORKER_ENV '$DEV/bin/mediaworker'"
	else
		warn "ffmpeg not found: photos and videos won't be processed (brew install ffmpeg, or make media-worker for the container)."
	fi
	start web "cd civic-console && pnpm dev"

	say "Waiting for the API…"
	wait_for api 60 curl -sf "$API_URL/v1/health" || { tail -20 "$LOGS/api.log"; fail "The API didn't come up (see .dev/logs/api.log)."; }
	say "Waiting for the web app…"
	wait_for web 90 curl -sf -o /dev/null "$WEB_URL/" || { tail -20 "$LOGS/web.log"; fail "The web app didn't come up (see .dev/logs/web.log)."; }
	for name in relay media; do
		[ -f "$PIDS/$name" ] && ! running "$name" && { tail -10 "$LOGS/$name.log"; warn "$name exited — see .dev/logs/$name.log"; }
	done

	echo
	say "Everything is up:"
	status
	echo
	echo "  App        $WEB_URL"
	echo "  API        $API_URL/v1/health"
	echo "  Media CDN  http://localhost:18080/media/variants/…"
	if [ -n "${AFRICASTALKING_API_KEY:-}" ]; then
		echo "  Login codes are sent by SMS (Africa's Talking)."
	else
		echo "  Login codes appear in .dev/logs/api.log (\"DEV SMS\")."
	fi
	echo "  Stop with: make dev-stop    Logs: make dev-logs"
	command -v open >/dev/null && open "$WEB_URL" || true
}

stop() {
	for name in web media relay api; do stop_one "$name"; done
	say "Stopped. Containers keep running; stop them with: docker compose --profile media stop"
}

status() {
	printf '  %-8s %s\n' "SERVICE" "STATE"
	for name in api relay media web; do
		if running "$name"; then printf '  %-8s running (pid %s)\n' "$name" "$(cat "$PIDS/$name")"
		else printf '  %-8s stopped\n' "$name"; fi
	done
	curl -sf "$API_URL/v1/health" >/dev/null 2>&1 && echo "  api health: ok" || echo "  api health: down"
	curl -sf -o /dev/null "$WEB_URL/" 2>/dev/null && echo "  web:        ok ($WEB_URL)" || echo "  web:        down"
}

logs() { tail -n 20 -F "$LOGS"/*.log; }

case "${1:-up}" in
	up) up ;;
	stop) stop ;;
	status) status ;;
	logs) logs ;;
	*) fail "usage: $0 up|stop|status|logs" ;;
esac
