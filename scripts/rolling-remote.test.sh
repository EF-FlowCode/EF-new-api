#!/bin/sh
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT
mkdir "$test_dir/bin"
cp "$script_dir/rolling-remote.sh" "$test_dir/rolling-remote.sh"
printf '#!/bin/sh\nprintf "started\\n" > "$TEST_RELEASE_TRACE"\nsleep 2\nprintf "completed\\n" >> "$TEST_RELEASE_TRACE"\n' > "$test_dir/bin/python3"
chmod +x "$test_dir/bin/python3"
export PATH="$test_dir/bin:$PATH"
export TEST_RELEASE_TRACE="$test_dir/trace"
cd "$test_dir"
sh ./rolling-remote.sh cleanup --root "$test_dir" > watcher.log 2>&1 &
watcher=$!
attempt=0
while [ ! -s "$TEST_RELEASE_TRACE" ]; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 10 ]
    sleep 1
done
kill -HUP "$watcher"
wait "$watcher" 2>/dev/null || :
attempt=0
while ! test -f .rolling/jobs/*/result; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 10 ]
    sleep 1
done
test "$(cat .rolling/jobs/*/result)" = 0
test "$(tail -n 1 "$TEST_RELEASE_TRACE")" = completed
printf 'PASS disconnected watcher leaves remote task running and records exit status\n'

# Same release id after a failed attempt: only a controller-approved retry
# starts a new job; the old job directory is kept as evidence.
cat > "$test_dir/bin/python3" <<'STUB'
#!/bin/sh
printf '%s\n' "$2" >> "$TEST_CALLS"
case "$2" in
    retryable) exit "$TEST_RETRYABLE" ;;
    activate) exit 0 ;;
esac
exit 9
STUB
chmod +x "$test_dir/bin/python3"
export TEST_CALLS="$test_dir/calls"
project="$test_dir/project"
jobs="$project/.rolling/jobs"
reset_job() {
    rm -rf "$jobs" "$TEST_CALLS"
    mkdir -p "$jobs/release-x"
    printf 'old attempt\n' > "$jobs/release-x/output.log"
    printf '%s\n' "$1" > "$jobs/release-x/result"
    : > "$TEST_CALLS"
}
ls_attempts() { find "$jobs" -maxdepth 1 -name 'release-x.attempt-*' | wc -l | tr -d ' '; }

reset_job 1
status=0
TEST_RETRYABLE=3 sh ./rolling-remote.sh release release-x "$project" > retry.log 2>&1 || status=$?
test "$status" = 1
test "$(cat "$TEST_CALLS")" = retryable
test "$(ls_attempts)" = 0
grep -q 'old attempt' retry.log
printf 'PASS refused retry replays the failed result without a new job\n'

reset_job 1
TEST_RETRYABLE=0 sh ./rolling-remote.sh release release-x "$project" > retry.log 2>&1
test "$(tr '\n' ' ' < "$TEST_CALLS")" = 'retryable activate '
test "$(ls_attempts)" = 1
test "$(cat "$jobs"/release-x.attempt-*/result)" = 1
test "$(cat "$jobs/release-x/result")" = 0
printf 'PASS approved retry archives the failed attempt and runs a new job\n'

reset_job 0
TEST_RETRYABLE=0 sh ./rolling-remote.sh release release-x "$project" > retry.log 2>&1
test ! -s "$TEST_CALLS"
test "$(ls_attempts)" = 0
printf 'PASS successful job is only observed\n'

reset_job 1
rm "$jobs/release-x/result"
sleep 30 &
running=$!
printf '%s\n' "$running" > "$jobs/release-x/pid"
(sleep 2; printf '1\n' > "$jobs/release-x/result") &
status=0
TEST_RETRYABLE=0 sh ./rolling-remote.sh release release-x "$project" > retry.log 2>&1 || status=$?
kill "$running" 2>/dev/null || :
test "$status" = 1
test ! -s "$TEST_CALLS"
test "$(ls_attempts)" = 0
printf 'PASS running job is observed, never restarted\n'
