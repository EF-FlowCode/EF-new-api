"""Worker impact analysis for blue-green planning; runs only inside Docker.

Blue-green keeps the running worker image. A backend change may skip the
maintenance window only when the analyzer (scripts/release-impact) proves that
nothing the worker can execute or use changed between the source the worker
was built from and the candidate tree. Any failure here is reported as an
error so the planner falls back to maintenance.
"""
import io
import json
from pathlib import Path
import subprocess
import tarfile

from release_core import ReleaseError, digest, file_hash, git

GO_IMAGE = 'golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba'
TOOL = 'scripts/release-impact'
TOOL_FILES = ('go.mod', 'go.sum', 'main.go', 'shared.go')
# Only Go sources and the files they embed matter; the web bundle is a
# placeholder because the analyzer type-checks without building assets.
EXCLUDED = ('web/', 'docs/', 'scripts/', '.releases/', '.ai-governance/')


def tool_digest(root):
    return digest({name: file_hash(Path(root) / TOOL / name) for name in TOOL_FILES})


def _add(archive, name, data):
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = 0o644
    archive.addfile(info, io.BytesIO(data))


def _source_tar(root, worker_revision):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w') as archive:
        exported = subprocess.run(['git', '-C', str(root), 'archive', '--format=tar', worker_revision],
                                  capture_output=True)
        if exported.returncode:
            raise ReleaseError('Cannot export the worker source revision ' + worker_revision)
        with tarfile.open(fileobj=io.BytesIO(exported.stdout)) as base:
            for member in base.getmembers():
                if member.isfile() and not member.name.startswith(EXCLUDED):
                    _add(archive, 'base/' + member.name, base.extractfile(member).read())
        # Same inventory rule as release fingerprints: tracked plus untracked, not ignored.
        names = git(root, 'ls-files', '-z', '--cached', '--others', '--exclude-standard').split('\0')
        for name in sorted(set(filter(None, names))):
            path = Path(root) / name
            if name.startswith(EXCLUDED) or path.is_symlink() or not path.is_file():
                continue
            _add(archive, 'head/' + name, path.read_bytes())
        _add(archive, 'head/web/dist/index.html', b'<!doctype html>')
        for name in TOOL_FILES:
            _add(archive, 'tool/' + name, (Path(root) / TOOL / name).read_bytes())
    return buffer.getvalue()


def analyze(root, worker_revision, timeout=900):
    """Return the analyzer report, or raise ReleaseError."""
    stream = _source_tar(root, worker_revision)
    script = ('set -e; mkdir -p /work; tar -xf - -C /work; cd /work/tool; '
              'go run . -base /work/base -head /work/head')
    try:
        result = subprocess.run(
            ['docker', 'run', '--rm', '-i', '--memory', '6g',
             '-v', 'newapi-gomod:/go/pkg/mod', '-v', 'newapi-gocache:/root/.cache/go-build',
             '-e', 'GOTOOLCHAIN=local', '-e', 'GOFLAGS=-mod=mod', GO_IMAGE, 'sh', '-c', script],
            input=stream, capture_output=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ReleaseError('Worker impact analysis did not run: ' + str(error)) from error
    if result.returncode:
        tail = result.stderr.decode('utf-8', 'replace').strip().splitlines()[-5:]
        raise ReleaseError('Worker impact analysis failed: ' + ' | '.join(tail))
    report = json.loads(result.stdout)
    if not isinstance(report.get('worker_affected'), list) or not isinstance(report.get('changed'), list):
        raise ReleaseError('Worker impact analysis returned an invalid report')
    return report
