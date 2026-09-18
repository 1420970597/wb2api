#!/bin/sh
# Prepare bind-mounted state as root, then run the gateway without privileges.
set -eu

PUID="${PUID:-10001}"
PGID="${PGID:-10001}"

case "$PUID:$PGID" in
  *[!0-9:]*|*:*:*)
    echo "PUID and PGID must be numeric (got PUID=$PUID PGID=$PGID)" >&2
    exit 64
    ;;
esac

mkdir -p /app/auths /app/data

# These are the only paths the gateway persists. A bind mount commonly arrives
# owned by the host user, so repair it before dropping privileges. On root-squash
# mounts chown can be denied; continue so an already-writable mount still works.
if ! chown -R "$PUID:$PGID" /app/auths /app/data; then
  echo "WARN: could not chown /app/auths or /app/data; verify the mount permits PUID=$PUID PGID=$PGID" >&2
fi
if ! find /app/auths /app/data -type d -exec chmod 700 {} +; then
  echo "WARN: could not restrict persistent directories to mode 700" >&2
fi
if ! find /app/auths /app/data -type f -exec chmod 600 {} +; then
  echo "WARN: could not restrict persistent files to mode 600" >&2
fi
exec su-exec "$PUID:$PGID" "$@"
