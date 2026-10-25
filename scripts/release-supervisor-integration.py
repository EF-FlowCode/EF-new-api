"""Exercise the real Linux supervisor with a deterministic synthetic controller.

Checks process lifetime and repeated observers, not application migration logic.
"""
import argparse
from pathlib import Path
import subprocess
import time
import uuid

from release import ROOT, run, save

CONTROLLER = '''#!/bin/sh
set -eu
# The supervisor asks the controller whether a failed job may run again; this
# synthetic controller always refuses, so a failed job is only observed.
case " $* " in *" retryable "*) printf 'retry refused\n' >> /fixture/retry-checks; exit 3 ;; esac
printf 'called\n' >> /fixture/calls
while [ ! -f /fixture/proceed ]; do sleep 1; done
if [ -f /fixture/fail ]; then exit 17; fi
printf 'controller completed\n'
'''
LAUNCH = '''#!/bin/sh
set -eu
echo $$ > /fixture/observer.pid
exec sh /controller/rolling-remote.sh release "$1" /fixture/project
'''


def main(directory):
    directory.mkdir(parents=True, exist_ok=False)
    (directory / 'bin').mkdir()
    (directory / 'project').mkdir()
    (directory / 'bin/python3').write_text(CONTROLLER, encoding='utf-8', newline='\n')
    (directory / 'launch.sh').write_text(LAUNCH, encoding='utf-8', newline='\n')
    name = 'release-supervisor-' + uuid.uuid4().hex[:12]
    try:
        run(['docker', 'run', '-d', '--name', name, '--network', 'none',
             '--mount', 'type=bind,source=' + str(directory.resolve()) + ',target=/fixture',
             '--mount', 'type=bind,source=' + str(ROOT / 'scripts') + ',target=/controller,readonly',
             '-e', 'PATH=/fixture/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
             'alpine:3.22', 'sleep', '300'])
        run(['docker', 'exec', name, 'chmod', '+x', '/fixture/bin/python3'])
        run(['docker', 'exec', '-d', name, 'sh', '/fixture/launch.sh', 'release-disconnect'])
        deadline = time.monotonic() + 10
        while not (directory / 'calls').exists():
            assert time.monotonic() < deadline
            time.sleep(0.1)
        pid = (directory / 'observer.pid').read_text().strip()
        assert pid.isdigit() and int(pid) > 1
        run(['docker', 'exec', name, 'kill', '-TERM', pid])
        (directory / 'proceed').touch()
        args = ['docker', 'exec', name, 'sh', '/controller/rolling-remote.sh', 'release',
                'release-disconnect', '/fixture/project']
        run(args, timeout=20)
        run(args, timeout=10)
        assert (directory / 'calls').read_text().splitlines() == ['called']
        assert (directory / 'project/.rolling/jobs/release-disconnect/result').read_text().strip() == '0'
        (directory / 'fail').touch()
        args[-2] = 'release-failure'
        for _ in range(2):
            result = subprocess.run(args, capture_output=True, timeout=20)
            assert result.returncode == 17
        assert len((directory / 'calls').read_text().splitlines()) == 2
        # The repeated observer consulted the retry gate, which refused.
        assert (directory / 'retry-checks').read_text().splitlines() == ['retry refused']
        stale = directory / 'project/.rolling/jobs/release-stale'
        stale.mkdir()
        args[-2] = 'release-stale'
        result = subprocess.run(args, capture_output=True, timeout=10)
        assert result.returncode == 1
        assert len((directory / 'calls').read_text().splitlines()) == 2
    finally:
        run(['docker', 'rm', '-f', name])
    save(directory / 'report.json', {'result': 'passed', 'observer_disconnect_survived': True,
        'repeat_observer_did_not_restart_controller': True, 'failure_exit_preserved': True,
        'stale_creation_refused': True, 'isolated_resources_cleaned': True})
    print('RELEASE_SUPERVISOR_PASSED')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence', type=Path, required=True)
    main(parser.parse_args().evidence)
