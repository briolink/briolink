#!/usr/bin/env bash
# Adds or updates one client in the shared Litestream daemon (#114). Run with sudo on the host:
#
#   ops/litestream-client.sh <client> <entry.yml> <keys.env>
#
# <entry.yml> is the client's `dbs:` list (the client repo's ops/litestream.yml); it names the
# keys it needs as ${<CLIENT>_LITESTREAM_ACCESS_KEY_ID} and ${<CLIENT>_LITESTREAM_SECRET_ACCESS_KEY}.
# <keys.env> holds LITESTREAM_ACCESS_KEY_ID and LITESTREAM_SECRET_ACCESS_KEY (the bucket-scoped
# token); they are copied into /etc/litestream/env (root-only) under the client's prefix, never
# passed on the command line. The script then
#   1. gives `backup` read and write on each database's folder and files (see the mask note),
#   2. creates the client's state folder (the entry's meta-path),
#   3. rebuilds /etc/litestream/litestream.yml from every entry in /etc/litestream/clients/,
#   4. checks that the new config parses and restarts the daemon.
# Safe to rerun: it replaces the client's entry and keys.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
[ $# -eq 3 ] || { sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
client=$1 entry=$2 keys=$3
[[ $client =~ ^[a-z][a-z0-9-]*$ ]] || { echo "client name: lower case letters, digits, dashes" >&2; exit 2; }
prefix=$(echo "$client" | tr 'a-z-' 'A-Z_')

dbs=$(awk '$1 == "-" && $2 == "path:" {print $3}' "$entry")
[ -n "$dbs" ] || { echo "no database path in $entry" >&2; exit 1; }
for db in $dbs; do
	[[ $db == /srv/$client/* ]] || { echo "$db is outside /srv/$client" >&2; exit 1; }
	[ -f "$db" ] || { echo "$db does not exist yet (start the client first)" >&2; exit 1; }
done

# 1. Access. The folder needs rwx (Litestream locks the file and checkpoints the WAL). SQLite
# gives -wal and -shm the database's mode, and with an ACL the group bits are the mask: a 0600
# database would mask `backup`'s rw away on its own sidecars. So the database is 0660 (the owner's
# group is the client's own user, with no shell) and the folder's default ACL covers new files.
setfacl -m u:backup:x "/srv/$client"
for db in $dbs; do
	dir=$(dirname "$db")
	setfacl -m u:backup:rwx "$dir"
	setfacl -d -m u:backup:rw "$dir"
	for f in "$db" "$db-wal" "$db-shm"; do
		[ -e "$f" ] || continue
		setfacl -m u:backup:rw "$f"
		chmod 0660 "$f"
	done
done

# 2. State and keys.
for meta in $(awk '$1 == "meta-path:" {print $2}' "$entry"); do
	install -d -m 0750 -o backup -g backup "$(dirname "$meta")"
done
get() { awk -F= -v k="$1" '$1 == k {sub(/^[^=]*=/, ""); print; exit}' "$keys"; }
id=$(get LITESTREAM_ACCESS_KEY_ID) secret=$(get LITESTREAM_SECRET_ACCESS_KEY)
[ -n "$id" ] && [ -n "$secret" ] || { echo "$keys needs LITESTREAM_ACCESS_KEY_ID and LITESTREAM_SECRET_ACCESS_KEY" >&2; exit 1; }
env=/etc/litestream/env
touch "$env"; chmod 0600 "$env"
grep -v "^${prefix}_LITESTREAM_" "$env" >"$env.new" || true
printf '%s_LITESTREAM_ACCESS_KEY_ID=%s\n%s_LITESTREAM_SECRET_ACCESS_KEY=%s\n' "$prefix" "$id" "$prefix" "$secret" >>"$env.new"
chmod 0600 "$env.new"; mv "$env.new" "$env"

# 3. Config.
install -m 0644 "$entry" "/etc/litestream/clients/$client.yml"
HERE=$(cd "$(dirname "$0")" && pwd)
"$HERE/litestream-merge.sh" /etc/litestream/clients /etc/litestream/litestream.yml.new

# 4. Check and restart.
(set -a; . "$env"; set +a; litestream databases -config /etc/litestream/litestream.yml.new)
mv /etc/litestream/litestream.yml.new /etc/litestream/litestream.yml
systemctl restart litestream
sleep 3
systemctl is-active litestream
