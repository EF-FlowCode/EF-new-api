#!/bin/sh
set -eu
umask 077
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# The supervisor survives SSH loss and leaves a result for later inspection.
if [ "${1:-}" = _run ]; then
    shift
    job=$1
    shift
    result=0
    if [ "${1:-}" = release ] || [ "${1:-}" = release-recover ]; then
        operation=activate
        if [ "$1" = release-recover ]; then operation=recover; fi
        shift
        python3 "$script_dir/release_remote.py" "$operation" --root "$1" --release "$script_dir" || result=$?
    else
        python3 "$script_dir/rolling-deploy.py" "$@" || result=$?
    fi
    printf '%s\n' "$result" > "$job/result.tmp"
    mv "$job/result.tmp" "$job/result"
    exit "$result"
fi

action=${1:?missing action}
shift
release_job=''
case "$action" in
    release)
        release_job=${1:?missing release id}
        case "$release_job" in release-*[!a-zA-Z0-9_.-]*|*[!a-zA-Z0-9_.-]*) exit 2 ;; release-*) ;; *) exit 2 ;; esac
        root=${2:?missing project root}
        cd "$root"
        set -- release "$root"
        ;;
    release-recover)
        root=${1:?missing project root}
        cd "$root"
        set -- release-recover "$root"
        ;;
    retire-rollback)
        if [ "${1:-}" != --root ] || [ "$#" -ne 6 ] || [ "${3:-}" != --expected-revision ] || [ "${5:-}" != --expected-image ]; then
            printf 'Explicit root, revision and image are required\n' >&2; exit 2
        fi
        cd "$2"
        set -- "$action" "$@"
        ;;
    rollback|cleanup)
        if [ "${1:-}" != --root ] || [ "$#" -ne 2 ]; then
            printf 'Explicit --root is required\n' >&2; exit 2
        fi
        cd "$2"
        set -- "$action" "$@"
        ;;
    status) exec python3 "$script_dir/rolling-deploy.py" status ;;
    *) printf 'Unsupported rolling action\n' >&2; exit 2 ;;
esac

mkdir -p .rolling/jobs
job=".rolling/jobs/${release_job:-$(date +%Y%m%d-%H%M%S)-$$}"
# A finished, failed attempt may run again under the same id only when the
# controller proves nothing was released or migrated (e.g. maintenance
# cancelled before migration). Running or successful jobs are only observed.
if [ -n "$release_job" ] && [ -f "$job/result" ] && [ "$(cat "$job/result")" != 0 ] &&
        python3 "$script_dir/release_remote.py" retryable --root "$root" --release "$script_dir" >&2; then
    attempt="$job.attempt-$(date +%Y%m%d-%H%M%S)"
    if [ -e "$attempt" ]; then
        printf 'Retry evidence %s already exists; retry later\n' "$attempt" >&2
        exit 1
    fi
    mv "$job" "$attempt"
    printf 'Previous attempt failed before migration; evidence kept in %s, starting a new job\n' "$attempt"
fi
if mkdir "$job" 2>/dev/null; then
    : > "$job/output.log"
    nohup sh "$script_dir/rolling-remote.sh" _run "$job" "$@" >> "$job/output.log" 2>&1 < /dev/null &
    printf '%s\n' "$!" > "$job/pid"
elif [ -z "$release_job" ]; then
    exit 2
fi
printf 'Release job: %s (continues if SSH disconnects)\n' "$job"
lines=0
while :; do
    if [ ! -f "$job/output.log" ]; then
        printf 'Job creation was interrupted; inspect %s before recovery\n' "$job" >&2
        exit 1
    fi
    current=$(wc -l < "$job/output.log")
    if [ "$current" -gt "$lines" ]; then
        sed -n "$((lines + 1)),${current}p" "$job/output.log"
        lines=$current
    fi
    if [ -f "$job/result" ]; then
        # Flush lines written between the last read and the result marker.
        sed -n "$((lines + 1)),\$p" "$job/output.log"
        exit "$(cat "$job/result")"
    fi
    if [ -f "$job/pid" ] && ! kill -0 "$(cat "$job/pid")" 2>/dev/null; then
        printf 'Supervisor stopped without a result; inspect and recover, do not rerun migration\n' >&2
        exit 1
    fi
    if [ ! -f "$job/pid" ] && [ ! -f "$job/result" ]; then
        printf 'Supervisor identity missing; inspect job before explicit recovery\n' >&2
        exit 1
    fi
    sleep 2
done
