#!/bin/bash
# Forced-command guard on the PVE host for the maintenance snapshot strategy.
# Installed as /usr/local/sbin/eflow-zfs-guard and bound to one key in
# /root/.ssh/authorized_keys:
#   restrict,from="<VM_IP>",command="/usr/local/sbin/eflow-zfs-guard" ssh-ed25519 ... eflow-zfs-guard
# The key can only snapshot one fixed zvol, inspect snapshots and attach a
# clone for restore drills. It can never roll back, destroy a maintenance
# snapshot or touch any other dataset, so a compromised VM cannot erase backups.
set -euo pipefail
DATASET=data/vmdata/vm-100-disk-1
VMID=100
DRILL_CLONE=data/vmdata/eflow-mysql-drill
DRILL_SLOT=scsi2
DRILL_PATH=/dev/zvol/$DRILL_CLONE

fail() { echo "{\"ok\":false,\"error\":\"$1\"}"; exit 1; }
read -r -a ARGS <<< "${SSH_ORIGINAL_COMMAND:-}"
[ "${#ARGS[@]}" -ge 1 ] || fail "missing command"
CMD=${ARGS[0]}
NAME=${ARGS[1]:-}
valid_name() { [[ "$NAME" =~ ^(maint-[0-9a-f]{32}|drill-[0-9]{14})$ ]]; }
snap_json() {
  local snap=$DATASET@eflow-$1
  zfs list -H -p -t snapshot -o guid,createtxg,creation,used "$snap" 2>/dev/null |
    awk -v n="$1" '{printf "{\"ok\":true,\"name\":\"%s\",\"guid\":\"%s\",\"createtxg\":\"%s\",\"creation\":%s,\"used\":%s}\n", n, $1, $2, $3, $4}'
}

case "$CMD" in
  status)
    [ "${#ARGS[@]}" -eq 1 ] || fail "unexpected arguments"
    avail=$(zfs list -H -p -o avail "$DATASET") || fail "dataset missing"
    count=$(zfs list -H -t snapshot -o name -d 1 "$DATASET" | grep -c "@eflow-" || true)
    guard=$(sha256sum "$0" | cut -d' ' -f1)
    echo "{\"ok\":true,\"dataset\":\"$DATASET\",\"avail\":$avail,\"snapshots\":$count,\"guard_sha256\":\"$guard\"}"
    ;;
  snapshot)
    [ "${#ARGS[@]}" -eq 2 ] && valid_name || fail "invalid snapshot name"
    zfs list -H -t snapshot "$DATASET@eflow-$NAME" >/dev/null 2>&1 && fail "snapshot exists"
    zfs snapshot "$DATASET@eflow-$NAME" || fail "snapshot failed"
    snap_json "$NAME"
    ;;
  verify)
    [ "${#ARGS[@]}" -eq 2 ] && valid_name || fail "invalid snapshot name"
    out=$(snap_json "$NAME") && [ -n "$out" ] || fail "snapshot missing"
    echo "$out"
    ;;
  drill-attach)
    [ "${#ARGS[@]}" -eq 2 ] && [[ "$NAME" =~ ^drill-[0-9]{14}$ ]] || fail "invalid drill name"
    zfs list -H "$DRILL_CLONE" >/dev/null 2>&1 && fail "drill clone already exists"
    qm config "$VMID" | grep -q "^$DRILL_SLOT:" && fail "drill slot busy"
    zfs clone -o primarycache=metadata "$DATASET@eflow-$NAME" "$DRILL_CLONE" || fail "clone failed"
    for _ in $(seq 1 30); do [ -e "$DRILL_PATH" ] && break; sleep 1; done
    [ -e "$DRILL_PATH" ] || fail "clone device missing"
    qm set "$VMID" --"$DRILL_SLOT" "$DRILL_PATH,discard=on,iothread=1,ssd=1" >/dev/null || fail "attach failed"
    echo "{\"ok\":true,\"serial\":\"drive-$DRILL_SLOT\"}"
    ;;
  drill-detach)
    [ "${#ARGS[@]}" -eq 2 ] && [[ "$NAME" =~ ^drill-[0-9]{14}$ ]] || fail "invalid drill name"
    if qm config "$VMID" | grep -q "^$DRILL_SLOT: $DRILL_PATH,"; then
      qm set "$VMID" --delete "$DRILL_SLOT" >/dev/null || fail "detach failed"
    fi
    qm config "$VMID" | grep -q "^$DRILL_SLOT:" && fail "drill slot still attached"
    if zfs list -H "$DRILL_CLONE" >/dev/null 2>&1; then
      [ "$(zfs get -H -o value origin "$DRILL_CLONE")" = "$DATASET@eflow-$NAME" ] || fail "unexpected clone origin"
      zfs destroy "$DRILL_CLONE" || fail "clone destroy failed"
    fi
    # Only drill snapshots are removable; maintenance snapshots are not.
    zfs list -H -t snapshot "$DATASET@eflow-$NAME" >/dev/null 2>&1 && { zfs destroy "$DATASET@eflow-$NAME" || fail "drill snapshot destroy failed"; }
    echo "{\"ok\":true}"
    ;;
  *)
    fail "unsupported command"
    ;;
esac
