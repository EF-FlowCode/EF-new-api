#!/usr/bin/env python3
"""Server-side rolling deployment. Uses only Python's standard library and Docker.

All resolved Compose credentials stay in the private server-side state directory.
Subprocess arguments/output that may contain credentials are never logged.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import shutil
import sys
import time
import urllib.error
import urllib.request
import urllib.parse
import uuid

CONTRACT = "eflow-rolling-v10"
PREFIX = "new-api-rolling"
GATEWAY_IMAGE = "nginx:1.28-alpine"
BACKUP_IMAGE = "mysql:8.4.8"
# zfs_snapshot is chosen by the release plan only with a passing restore drill.
BACKUP_STRATEGIES = ("stopped_full", "zfs_snapshot")
# Announced maintenance: in-flight requests get this long to finish before the
# gateway closes the remaining client connections. Settlement afterwards keeps
# the full drain budget (Deployment.wait).
MAINTENANCE_GRACE_SECONDS = 10
GATEWAY_ORIGIN = "http://127.0.0.1:3000"
SNAPSHOT_CONFIG_KEYS = ("ssh_host", "ssh_user", "identity", "known_hosts", "dataset",
                        "mount", "fs_uuid", "mysql_container", "min_pool_free_bytes")


class DeployError(RuntimeError):
    pass


def log(message):
    print(time.strftime("[%H:%M:%S] ") + message, flush=True)


def atomic_json(path, data):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + ".tmp")
    with open(temporary, "w", encoding="utf-8") as output:
        os.chmod(temporary, 0o600)
        json.dump(data, output, ensure_ascii=False, indent=2)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)


def run(*args, timeout=120):
    result = subprocess.run(args, capture_output=True, text=True, encoding="utf-8", timeout=timeout)
    if result.returncode:
        # Docker/Compose can echo resolved environment variables in errors.
        raise DeployError(f"{args[0]} operation failed (exit {result.returncode}); inspect container status locally")
    return result.stdout


def nginx_config(instance):
    if not re.fullmatch(r"[a-zA-Z0-9_.-]+", instance):
        raise DeployError("invalid upstream instance")
    # Keep each slot's shared DNS state separate while old workers drain.
    upstream = "web_" + hashlib.sha256(instance.encode()).hexdigest()[:16]
    return """worker_processes 2;
events { worker_connections 8192; }
http {
    recursive_error_pages on;
    access_log off;
    map $http_upgrade $connection_upgrade { default upgrade; '' ''; }
    map $http_x_forwarded_for $forwarded_for { default $http_x_forwarded_for; '' $remote_addr; }
    map $http_x_forwarded_proto $forwarded_proto { default $http_x_forwarded_proto; '' $scheme; }
    map $uri $maintenance_api {
        default 0;
        ~^/(api|v[0-9]+[a-z]*|mj|suno|pg|task)(/|$) 1;
    }
    map "$request_method:$maintenance_api:$http_accept" $maintenance_page {
        default 0;
        ~^GET:0:.*text/html 1;
    }
    upstream UPSTREAM_NAME {
        zone UPSTREAM_NAME 64k;
        resolver 127.0.0.11 valid=1s ipv6=off;
        resolver_timeout 2s;
        server UPSTREAM_HOST:3000 resolve;
    }
    server {
        listen 3000;
        client_max_body_size 0;
        location = /__maintenance/status {
            default_type application/json;
            add_header Cache-Control "no-store" always;
            if (-f /etc/nginx/maintenance.flag) { return 200 '{"maintenance":true,"retry_after":30}'; }
            return 200 '{"maintenance":false}';
        }
        location = /__maintenance/page.js {
            alias /etc/nginx/maintenance-page.js;
            default_type application/javascript;
            add_header Cache-Control "no-store" always;
        }
        location = /__maintenance/page.html {
            internal;
            alias /etc/nginx/maintenance.html;
            default_type text/html;
            add_header Cache-Control "no-store" always;
            add_header Retry-After 30 always;
            add_header X-Eflow-Maintenance 1 always;
        }
        location @maintenance {
            default_type application/json;
            add_header Cache-Control "no-store" always;
            add_header Retry-After 30 always;
            add_header X-Eflow-Maintenance 1 always;
            error_page 418 =503 /__maintenance/page.html;
            if ($maintenance_page) { return 418; }
            return 503 '{"success":false,"message":"Service temporarily under maintenance. Please retry later.","error":{"type":"service_unavailable","code":"maintenance","message":"Service temporarily under maintenance. Please retry later."}}';
        }
        location ~ "^/(static|assets)/(?:[A-Za-z0-9_@+-]+/)*[A-Za-z0-9_@+.-]*[.-][a-f0-9]{8,64}(?:[.][A-Za-z0-9]+)+$" {
            error_page 503 = @maintenance;
            if (-f /etc/nginx/maintenance.flag) { return 503; }
            root /etc/nginx/retained-assets;
            try_files $uri @application;
            limit_except GET { deny all; }
            types {
                application/javascript js;
                text/css css;
                font/woff woff;
                font/woff2 woff2;
                image/png png;
                image/jpeg jpg jpeg;
                image/svg+xml svg;
                image/webp webp;
                image/gif gif;
                application/wasm wasm;
            }
            default_type application/octet-stream;
            gzip on;
            gzip_vary on;
            gzip_min_length 1024;
            gzip_comp_level 5;
            gzip_proxied any;
            gzip_types application/javascript text/css image/svg+xml application/wasm;
            add_header Cache-Control "public, max-age=31536000, immutable";
            add_header X-Content-Type-Options nosniff always;
        }
        location / {
            PROXY_APPLICATION
        }
        location @application {
            PROXY_APPLICATION
        }
    }
}
""".replace("PROXY_APPLICATION", """
            error_page 503 = @maintenance;
            if (-f /etc/nginx/maintenance.flag) { return 503; }
            proxy_pass http://UPSTREAM_NAME;
            proxy_http_version 1.1;
            proxy_set_header Host $http_host;
            proxy_set_header X-Forwarded-For $forwarded_for;
            proxy_set_header X-Forwarded-Proto $forwarded_proto;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_buffering off;
            proxy_request_buffering off;
            proxy_next_upstream off;
            proxy_read_timeout 86400s;
            proxy_send_timeout 86400s;
""").replace("UPSTREAM_HOST", instance).replace("UPSTREAM_NAME", upstream)


def verify_auth_environment(before, after):
    """Only compare secrets in memory; never include their values in errors."""
    if not before.get("SESSION_SECRET") or not after.get("SESSION_SECRET"):
        raise DeployError("SESSION_SECRET must be fixed and configured before switching")
    for key in ("SESSION_SECRET", "EFLOW_AUTH_SECRET", "SESSION_COOKIE_SECURE"):
        if str(before.get(key, "")) != str(after.get(key, "")):
            raise DeployError("authentication configuration mismatch: " + key)


class Deployment:
    def __init__(self, root, wait=120, grace=MAINTENANCE_GRACE_SECONDS):
        self.root = Path(root).resolve()
        self.directory = self.root / ".rolling"
        self.directory.mkdir(mode=0o700, exist_ok=True)
        self.state_path = self.directory / "state.json"
        self.compose_path = self.directory / "compose.json"
        self.wait = wait
        self.drain_grace = grace
        self.gateway_origin = GATEWAY_ORIGIN
        self.state = json.loads(self.state_path.read_text(encoding="utf-8")) if self.state_path.exists() else {}

    def save(self, **values):
        self.state.update(values)
        atomic_json(self.state_path, self.state)

    def compose(self, *args, timeout=120):
        return run("docker", "compose", "-p", PREFIX, "-f", str(self.compose_path), *args, timeout=timeout)

    def name(self, slot):
        return PREFIX + "-" + slot

    def inspect(self, name):
        return json.loads(run("docker", "inspect", name))[0]

    def running(self, slot):
        if not run("docker", "ps", "-aq", "--filter", "name=^/" + self.name(slot) + "$").strip():
            return False
        return self.inspect(self.name(slot))["State"]["Running"]

    def control(self, slot, path="ready", post=False):
        args = ["docker", "exec", self.name(slot), "wget", "-q", "-T", "5", "-O", "-"]
        if post:
            args += ["--post-data="]
        return json.loads(run(*args, "http://127.0.0.1:3001/" + path, timeout=10))

    def verify_auth_continuity(self, source, target=None, planned=False):
        container = self.inspect(self.name(source))
        before = dict(value.split("=", 1) for value in container["Config"]["Env"] if "=" in value)
        if planned:
            config = json.loads(self.compose_path.read_text(encoding="utf-8"))
            for slot in ("blue", "green"):
                verify_auth_environment(before, config["services"][slot]["environment"])
        else:
            candidate = self.inspect(self.name(target))
            after = dict(value.split("=", 1) for value in candidate["Config"]["Env"] if "=" in value)
            verify_auth_environment(before, after)

    def ready(self, slot, timeout=90, expected_contract=None):
        expected_contract = expected_contract or CONTRACT
        deadline = time.monotonic() + timeout
        while True:
            try:
                info = self.control(slot)
                expected_role = "worker" if slot == "worker" else "web"
                if info.get("contract") != expected_contract or info.get("role") != expected_role:
                    raise DeployError("deployment contract or role mismatch; maintenance upgrade required")
                if info.get("instance") != self.name(slot):
                    raise DeployError("instance identity mismatch")
                if info.get("ready"):
                    return info
            except (DeployError, subprocess.TimeoutExpired, ValueError):
                pass
            if time.monotonic() >= deadline:
                detail = "maintenance has not restored traffic; fix the cause and run recover" if self.state.get("maintenance_context") else "current traffic was not changed"
                raise DeployError(f"{slot} readiness failed; {detail}") from None
            time.sleep(1)

    def configure_image(self, slot, image):
        config = json.loads(self.compose_path.read_text(encoding="utf-8"))
        config["services"][slot]["image"] = image
        atomic_json(self.compose_path, config)

    def write_route(self, slot):
        directory = self.directory / "nginx"
        directory.mkdir(exist_ok=True)
        directory.chmod(0o755)  # This mount contains only public gateway assets.
        for source, target in (("maintenance.html", "maintenance.html"), ("maintenance-page.js", "maintenance-page.js")):
            (directory / target).write_bytes(Path(__file__).with_name(source).read_bytes())
            (directory / target).chmod(0o644)
        temporary = directory / "nginx.conf.tmp"
        temporary.write_text(nginx_config(self.name(slot)), encoding="utf-8")
        os.replace(temporary, directory / "nginx.conf")

    def reload(self, slot):
        self.write_route(slot)
        run("docker", "exec", self.name("gateway"), "nginx", "-t")
        run("docker", "exec", self.name("gateway"), "nginx", "-s", "reload")

    def verify_route(self, slot, timeout=15):
        deadline = time.monotonic() + timeout
        # Never use proxy environment variables for the loopback probe.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        while time.monotonic() < deadline:
            try:
                request = urllib.request.Request(self.gateway_origin + "/api/status", headers={"Connection": "close"})
                with opener.open(request, timeout=4) as response:
                    if response.headers.get("X-New-Api-Instance") == self.name(slot):
                        return
            except OSError:
                pass
            time.sleep(0.5)
        raise DeployError("gateway did not confirm the intended instance")

    def gateway_retiring(self):
        processes = run("docker", "exec", self.name("gateway"), "ps", "-o", "args")
        return "worker process is shutting down" in processes

    def switch(self, target, release_state=None):
        source = self.state["active"]
        self.verify_auth_continuity(source, target)
        self.retain_assets(source, target)
        self.save(phase="switching", switching_from=source, switching_to=target)
        try:
            self.reload(target)
            self.verify_route(target)
        except Exception:
            # Resume is idempotent, including a rollback after a prior drain.
            self.control(source, "resume", True)
            self.ready(source)
            self.reload(source)
            self.verify_route(source)
            self.save(phase="stable", active=source, pending_cleanup=target)
            raise
        self.save(phase="stable", active=target, previous=source, pending_cleanup=source, **(release_state or {}))
        log(f"NEW TRAFFIC ACTIVE: {target}; old requests continue on {source}")

    def retain_assets(self, *slots):
        from static_assets import retain_container
        for slot in slots:
            info = self.inspect(self.name(slot))
            manifest = retain_container(self.name(slot), info['Image'], self.directory / 'nginx/retained-assets')
            log(f"STATIC ASSETS RETAINED: {slot}, {len(manifest['assets'])} verified files")

    def cleanup(self):
        source = self.state.get("pending_cleanup")
        if not source or source == self.state.get("active"):
            return
        if not self.running(source):
            self.save(pending_cleanup=None)
            return
        deadline = time.monotonic() + self.wait
        while True:
            # Old nginx workers may still own SSE/WebSocket connections. They
            # must exit before the old application's admission gate is closed.
            if not self.gateway_retiring():
                info = self.control(source, "drain", True)
                if info["active"] == 0 and info["pending"] == 0:
                    self.control(source, "quiesce", True)
                    # No docker stop timeout and no SIGKILL fallback.
                    run("docker", "update", "--restart=no", self.name(source))
                    run("docker", "kill", "--signal=TERM", self.name(source))
                    exit_code = run("docker", "wait", self.name(source), timeout=30).strip()
                    if exit_code != "0":
                        raise DeployError(f"{source} exited unexpectedly with code {exit_code}; inspect logs")
                    self.save(pending_cleanup=None)
                    log(f"DRAIN COMPLETE: {source}; stopped without interrupting requests")
                    return
            if time.monotonic() >= deadline:
                log(f"NEW VERSION IS SERVING. Retaining {source}; run cleanup later. No requests were killed.")
                return
            time.sleep(1)

    def retire_rollback(self, expected_revision, expected_image):
        """Archive an incompatible rollback pointer before an online data change."""
        self.require_stable()
        active = self.state.get("active")
        if (not expected_revision or not expected_image or active not in ("blue", "green")
                or self.state.get("release_revision") != expected_revision
                or self.inspect(self.name(active))["Image"] != expected_image):
            raise DeployError("active release does not match rollback retirement request")
        self.cleanup()
        if self.state.get("pending_cleanup") or self.gateway_retiring():
            raise DeployError("old requests must drain before retiring rollback")
        self.ready(active)
        self.verify_route(active)
        previous = self.state.get("previous")
        if not previous:
            return
        if previous == active or self.running(previous):
            raise DeployError("rollback instance must be stopped before retirement")
        atomic_json(self.directory / ("retired-rollback-" + uuid.uuid4().hex + ".json"), self.state)
        self.save(previous=None, previous_release=None)
        log("ROLLBACK RETIRED: archived previous identity; active web and worker unchanged")

    def load_image(self, archive, image):
        if not re.fullmatch(r"new-api-eflow:release-[A-Za-z0-9_.-]+", image) and not (
                archive is None and re.fullmatch(r"sha256:[a-f0-9]{64}", image)):
            raise DeployError("an immutable release tag is required")
        if archive is not None:
            log("Loading release image")
            run("docker", "load", "--input", str(Path(archive).resolve()), timeout=300)
        return json.loads(run("docker", "image", "inspect", image))[0]["Id"]

    def deploy(self, archive, image):
        self.require_stable()
        self.cleanup()
        if self.state.get("pending_cleanup"):
            raise DeployError("previous instance is still draining; two-slot deployment cannot reuse it")
        self.ready("worker")
        self.ready(self.state["active"])
        self.verify_route(self.state["active"])
        target = "green" if self.state["active"] == "blue" else "blue"
        immutable = self.load_image(archive, image)
        self.verify_auth_continuity(self.state["active"], planned=True)
        self.configure_image(target, immutable)
        self.save(phase="preparing", candidate=target, candidate_image=immutable, previous=None)
        try:
            self.compose("up", "-d", "--no-deps", "--force-recreate", target)
            self.ready(target)
        except Exception:
            self.save(phase="stable", pending_cleanup=target)
            raise
        self.switch(target)
        log(f"Worker retained at {self.state['worker_image'][:19]}; no duplicate global jobs")
        self.cleanup()

    def handoff_worker(self, image):
        """Replace the worker in place under the same contract; web traffic is untouched.

        The old worker drains, quiesces and exits before the new one starts, so
        the shared /data lock never sees two workers. Background loops pause for
        the gap. Any failure restarts the old image and leaves the caller to
        roll back web; nothing here touches the database.
        """
        self.require_stable()
        immutable = self.load_image(None, image)
        old = self.inspect(self.name("worker"))["Image"]
        if old == immutable:
            return
        self.ready("worker")
        self.save(phase="worker_handoff", worker_handoff={"from": old, "to": immutable})
        self.control("worker", "drain", True)
        deadline = time.monotonic() + self.wait
        while True:
            status = self.control("worker", "status")
            if status["active"] == 0 and status["pending"] == 0:
                break
            if time.monotonic() >= deadline:
                self.control("worker", "resume", True)
                self.ready("worker")
                self.save(phase="stable", worker_handoff=None)
                raise DeployError("worker drain timed out; old worker resumed and kept")
            time.sleep(1)
        self.control("worker", "quiesce", True)
        self.stop_quiesced("worker")
        try:
            self.start_worker(immutable)
        except Exception:
            self.restore_worker(old)
            raise DeployError("new worker failed readiness; old worker restored") from None
        self.save(phase="stable", worker_image=immutable, worker_handoff=None)
        log(f"WORKER HANDOFF COMPLETE: {old[:19]} -> {immutable[:19]}")

    def start_worker(self, image):
        self.configure_image("worker", image)
        self.compose("up", "-d", "--no-deps", "--force-recreate", "worker")
        run("docker", "update", "--restart=unless-stopped", self.name("worker"))
        self.ready("worker")

    def restore_worker(self, image):
        """Put a known worker image back: never two workers, never SIGKILL a live one."""
        if self.running("worker"):
            self.control("worker", "drain", True)
            deadline = time.monotonic() + self.wait
            while True:
                status = self.control("worker", "status")
                if status["active"] == 0 and status["pending"] == 0:
                    break
                if time.monotonic() >= deadline:
                    raise DeployError("candidate worker still owns work; worker handoff left for recover")
                time.sleep(1)
            self.control("worker", "quiesce", True)
            self.stop_quiesced("worker")
        elif run("docker", "ps", "-aq", "--filter", "name=^/" + self.name("worker") + "$").strip():
            # A crashed candidate must not be revived by its restart policy.
            run("docker", "update", "--restart=no", self.name("worker"))
        self.start_worker(image)
        self.save(phase="stable", worker_handoff=None)
        log(f"WORKER RESTORED: {image[:19]}")

    def maintenance(self, archive, image, backup_strategy="stopped_full"):
        """Perform a stop-the-world release for incompatible data semantics."""
        if backup_strategy not in BACKUP_STRATEGIES:
            raise DeployError("unsupported maintenance backup strategy")
        self.require_stable()
        self.cleanup()
        if self.state.get("pending_cleanup"):
            raise DeployError("maintenance release requires completed cleanup")
        if self.state.get("previous"):
            previous = self.state["previous"]
            if self.running(previous) or self.gateway_retiring():
                raise DeployError("rollback slot still has connections; cannot retire it")
            atomic_json(self.directory / ("retired-rollback-" + uuid.uuid4().hex + ".json"), self.state)
            self.save(previous=None)
        source = self.state.get("active")
        if source not in ("blue", "green"):
            raise DeployError("maintenance release requires an active web slot")

        old_contract = self.state.get("contract") or CONTRACT
        self.ready("worker", expected_contract=old_contract)
        self.ready(source, expected_contract=old_contract)
        self.verify_route(source)
        immutable = self.load_image(archive, image)
        # Fail missing backup tooling before closing admission. All snapshot
        # files are private and are never included in the public job output.
        run("docker", "image", "inspect", BACKUP_IMAGE)
        self.verify_auth_continuity(source, planned=True)
        if backup_strategy == "zfs_snapshot":
            self.check_snapshot_target()
        else:
            self.check_backup_capacity()
        context = self.directory / ("maintenance-" + uuid.uuid4().hex)
        context.mkdir(mode=0o700)
        config = json.loads(self.compose_path.read_text(encoding="utf-8"))
        snapshot = {"state": copy.deepcopy(self.state), "config": config,
                    "images": {slot: self.inspect(self.name(slot))["Image"] for slot in (source, "worker")}}
        atomic_json(context / "original.json", snapshot)
        self.save(phase="maintenance", maintenance_source=source, maintenance_image=immutable,
                  maintenance_context=context.name, migration_started=False,
                  migration_container=None, migration_failed=False, maintenance_backup=None,
                  maintenance_backup_strategy=backup_strategy, maintenance_forced_cut=None)
        try:
            self.prepare_maintenance()
        except Exception:
            # The write-ahead boundary below proves no migration was issued.
            # Resume running instances in place, preserving any long requests.
            self.resume_before_migration()
            raise
        self.continue_maintenance()

    def maintenance_directory(self):
        name = self.state.get("maintenance_context", "")
        if not re.fullmatch(r"maintenance-[0-9a-f]{32}", name):
            raise DeployError("maintenance recovery metadata is missing; do not guess the database state")
        return self.directory / name

    def check_backup_capacity(self):
        """Read current database size while traffic is still admitted."""
        config = json.loads(self.compose_path.read_text(encoding="utf-8"))
        env = config["services"]["worker"]["environment"]
        total = 0
        for dsn in set(filter(None, (env.get("SQL_DSN"), env.get("LOG_SQL_DSN")))):
            match = re.fullmatch(r"([^:]+):(.*)@tcp\(([^)]+)\)/([^?]+)(?:\?(.*))?", dsn)
            if not match:
                raise DeployError("capacity preflight requires a supported TCP MySQL DSN")
            user, password, address, database, parameters = match.groups()
            tls = urllib.parse.parse_qs(parameters or "").get("tls", ["false"])[0]
            if tls not in ("true", "false", "skip-verify", "preferred"):
                raise DeployError("custom MySQL TLS requires an explicit backup client configuration")
            ssl_mode = {"true": "VERIFY_IDENTITY", "false": "DISABLED", "skip-verify": "REQUIRED", "preferred": "PREFERRED"}[tls]
            host, port = address.rsplit(":", 1) if ":" in address else (address, "3306")
            query = "SELECT COALESCE(SUM(DATA_LENGTH+INDEX_LENGTH),0) FROM information_schema.tables WHERE TABLE_SCHEMA=CONVERT(0x" + urllib.parse.unquote(database).encode().hex() + " USING utf8mb4)"
            client = {"services": {"capacity": {"image": BACKUP_IMAGE,
                "entrypoint": ["mysql", "--host=" + host.strip("[]"), "--port=" + port, "--user=" + user,
                               "--ssl-mode=" + ssl_mode, "--batch", "--skip-column-names", "-e", query],
                "environment": {"MYSQL_PWD": password}, "networks": list(config["services"]["worker"]["networks"])}},
                "networks": config["networks"]}
            path = self.directory / "backup-capacity-client.json"
            atomic_json(path, client)
            total += int(run("docker", "compose", "-p", PREFIX, "-f", str(path), "run", "--rm", "-T", "--no-deps", "capacity"))
        if total <= 0:
            raise DeployError("database size unavailable; refusing to guess backup capacity")
        # Logical SQL and indexes differ; leave a conservative expansion margin.
        needed = total * 3 + 2 * 1024 ** 3
        if shutil.disk_usage(self.root).free < needed:
            raise DeployError("not enough free space for a complete backup; traffic remains online")
        log(f"BACKUP PREFLIGHT: estimated database bytes={total}, reserved free space={needed}")

    def snapshot_config(self):
        path = self.directory / "backup-snapshot.json"
        if not path.exists():
            raise DeployError("snapshot backup is not configured on this server; traffic remains online")
        config = json.loads(path.read_text(encoding="utf-8"))
        if config.get("schema") != 1 or any(key not in config for key in SNAPSHOT_CONFIG_KEYS):
            raise DeployError("invalid snapshot backup configuration")
        return config

    def snapshot_guard(self, *args, timeout=30):
        """Call the host forced command; it accepts only its fixed verbs."""
        config = self.snapshot_config()
        completed = subprocess.run(
            ["ssh", "-i", config["identity"], "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
             "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + config["known_hosts"],
             "-o", "ConnectTimeout=10", config["ssh_user"] + "@" + config["ssh_host"], " ".join(args)],
            capture_output=True, text=True, encoding="utf-8", timeout=timeout)
        try:
            result = json.loads(completed.stdout.strip().splitlines()[-1])
        except (IndexError, ValueError):
            raise DeployError(f"snapshot guard unreachable for {args[0]} (exit {completed.returncode})") from None
        if completed.returncode or not result.get("ok"):
            raise DeployError(f"snapshot guard refused {args[0]}: {result.get('error')}")
        return result

    def check_snapshot_target(self):
        """Prove the snapshotted zvol holds every database the release backs up."""
        config = self.snapshot_config()
        mount = config["mount"]
        source = run("findmnt", "-n", "-o", "SOURCE", "--mountpoint", mount).strip()
        if not source or run("blkid", "-s", "UUID", "-o", "value", source).strip() != config["fs_uuid"]:
            raise DeployError("database directory is not on the configured snapshot disk")
        container = self.inspect(config["mysql_container"])
        binds = {m["Destination"]: m["Source"] for m in container["Mounts"]}
        if not container["State"]["Running"] or binds.get("/var/lib/mysql") != mount:
            raise DeployError("MySQL datadir does not match the snapshot disk")
        env = json.loads(self.compose_path.read_text(encoding="utf-8"))["services"]["worker"]["environment"]
        for dsn in set(filter(None, (env.get("SQL_DSN"), env.get("LOG_SQL_DSN")))):
            match = re.fullmatch(r"[^:]+:.*@tcp\(([^)]+)\)/[^?]+(?:\?.*)?", dsn)
            host = match.group(1).rsplit(":", 1)[0].strip("[]") if match else None
            if host != config["mysql_container"]:
                raise DeployError("a configured database is outside the snapshot disk")
        status = self.snapshot_guard("status")
        if status.get("dataset") != config["dataset"] or int(status["avail"]) < int(config["min_pool_free_bytes"]):
            raise DeployError("snapshot pool lacks reserved free space; traffic remains online")
        log(f"BACKUP PREFLIGHT: snapshot dataset={config['dataset']} pool_avail={status['avail']}")
        return config

    def snapshot_database(self, name):
        """Freeze only the database filesystem; always thaw before returning."""
        config = self.check_snapshot_target()
        mount = config["mount"]
        run("sync", "-f", mount)
        started = time.monotonic()
        run("fsfreeze", "--freeze", mount)
        try:
            result = self.snapshot_guard("snapshot", name)
        finally:
            try:
                run("fsfreeze", "--unfreeze", mount)
            except DeployError:
                raise DeployError("CRITICAL: database filesystem may still be frozen; run fsfreeze --unfreeze " + mount) from None
        result.update(dataset=config["dataset"], fs_uuid=config["fs_uuid"], mount=mount,
                      frozen_seconds=round(time.monotonic() - started, 3))
        return result

    def original_maintenance(self):
        return json.loads((self.maintenance_directory() / "original.json").read_text(encoding="utf-8"))

    def prepare_maintenance(self):
        source = self.state["maintenance_source"]
        log(f"MAINTENANCE: draining {source} and worker before billing migration")
        # Install while healthy, then use a file gate observed by ALL nginx
        # workers, including older workers retaining long connections.
        self.reload(source)
        self.set_maintenance(True)
        self.save(maintenance_step="draining")
        self.control(source, "drain", True)
        self.control("worker", "drain", True)
        self.drain_maintenance(source)
        self.control(source, "quiesce", True)
        self.control("worker", "quiesce", True)
        for slot in (source, "worker"):
            self.stop_quiesced(slot)
        self.save(maintenance_step="backup")
        self.backup_maintenance()
        # Persist intent BEFORE creating a migration container. After this point
        # neither exceptions nor recover may start an old binary or restore DB.
        self.save(phase="migrating", migration_started=True, maintenance_step="migrating")

    def drain_status(self, source):
        web = self.control(source, "status")
        worker = self.control("worker", "status")
        drained = web["active"] == 0 and web["pending"] == 0 and worker["active"] == 0 and worker["pending"] == 0
        return web, worker, drained

    def drain_maintenance(self, source):
        """Announced maintenance: short grace, gateway cut, then wait for settlement.

        Only client connections are closed. Web and worker processes keep
        running, so relays observe the disconnect and refund or settle. Only a
        settlement timeout cancels the release, which is still before migration.
        """
        started = time.monotonic()
        while True:
            web, worker, drained = self.drain_status(source)
            elapsed = time.monotonic() - started
            if drained:
                log(f"MAINTENANCE DRAINED: naturally in {elapsed:.1f}s (grace {self.drain_grace}s); no connection was cut")
                return
            if elapsed >= self.drain_grace:
                break
            time.sleep(min(1, max(0, self.drain_grace - elapsed)))
        grace = time.monotonic() - started
        if web["active"] > 0:
            log(f"MAINTENANCE FORCED CUT: active={web['active']} pending={web['pending']} "
                f"worker_active={worker['active']} worker_pending={worker['pending']} after {grace:.0f}s grace")
            self.save(maintenance_forced_cut={"active": web["active"], "pending": web["pending"],
                                              "worker_active": worker["active"], "worker_pending": worker["pending"],
                                              "grace_seconds": round(grace, 1)})
            cut_started = time.monotonic()
            self.cut_gateway_connections()
            log(f"MAINTENANCE GATEWAY RESTARTED: client connections closed in {time.monotonic() - cut_started:.1f}s; "
                "maintenance page still served; waiting for settlement")
        else:
            log(f"MAINTENANCE SETTLING: no client connection left after {grace:.0f}s grace; "
                f"pending={web['pending']} worker_active={worker['active']} worker_pending={worker['pending']}")
        settle_started = time.monotonic()
        deadline = settle_started + self.wait
        while True:
            web, worker, drained = self.drain_status(source)
            if drained:
                log(f"MAINTENANCE DRAINED: grace {grace:.1f}s, settlement {time.monotonic() - settle_started:.1f}s")
                return
            if time.monotonic() >= deadline:
                raise DeployError(f"maintenance settlement timed out after {self.wait}s (web active={web['active']} "
                                  f"pending={web['pending']}, worker active={worker['active']} pending={worker['pending']}); "
                                  "instances remain stopped from new work")
            time.sleep(1)

    def cut_gateway_connections(self):
        """Restart only the gateway; the bind-mounted flag keeps serving maintenance."""
        if not (self.directory / "nginx" / "maintenance.flag").is_file():
            raise DeployError("maintenance flag missing; refusing to cut gateway connections")
        run("docker", "restart", "-t", "10", self.name("gateway"), timeout=60)
        self.verify_maintenance_gate()

    def verify_maintenance_gate(self, timeout=15):
        """New requests through the restarted gateway must get the maintenance response."""
        deadline = time.monotonic() + timeout
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        while time.monotonic() < deadline:
            request = urllib.request.Request(self.gateway_origin + "/api/status", headers={"Connection": "close"})
            try:
                with opener.open(request, timeout=4):
                    pass
            except urllib.error.HTTPError as error:
                if error.code == 503 and error.headers.get("X-Eflow-Maintenance") == "1":
                    return
            except OSError:
                pass
            time.sleep(0.5)
        raise DeployError("restarted gateway did not serve the maintenance response")

    def set_maintenance(self, enabled):
        directory = self.directory / "nginx"
        directory.mkdir(exist_ok=True)
        flag = directory / "maintenance.flag"
        if enabled:
            flag.touch()
            flag.chmod(0o644)
        else:
            flag.unlink(missing_ok=True)

    def stop_quiesced(self, slot):
        # Never let docker stop's timeout turn a slow shutdown into SIGKILL.
        run("docker", "update", "--restart=no", self.name(slot))
        run("docker", "kill", "--signal=TERM", self.name(slot))
        if run("docker", "wait", self.name(slot), timeout=120).strip() != "0":
            raise DeployError(f"{slot} did not exit cleanly; inspect before recovery")

    def restore_route(self, slot):
        self.reload(slot)
        self.set_maintenance(False)
        try:
            self.verify_route(slot)
        except Exception:
            self.set_maintenance(True)
            raise

    def resume_before_migration(self):
        if self.state.get("migration_started"):
            raise DeployError("migration may have changed data; old-version recovery is forbidden")
        original = self.original_maintenance()
        source = self.state["maintenance_source"]
        config = original["config"]
        stopped = []
        for slot in ("worker", source):
            if self.running(slot):
                if self.inspect(self.name(slot))["Image"] != original["images"][slot]:
                    raise DeployError("old instance image changed; cannot resume it safely")
            else:
                config["services"][slot]["image"] = original["images"][slot]
                stopped.append(slot)
        atomic_json(self.compose_path, config)
        if stopped:
            self.compose("up", "-d", "--no-deps", *stopped)
        for slot in ("worker", source):
            deadline = time.monotonic() + 90
            while True:
                try:
                    self.control(slot, "resume", True)
                    break
                except (DeployError, ValueError, subprocess.TimeoutExpired):
                    if time.monotonic() >= deadline:
                        raise DeployError("old instance could not resume; retry recover after fixing dependencies") from None
                    time.sleep(1)
            self.ready(slot, expected_contract=original["state"].get("contract", CONTRACT))
            run("docker", "update", "--restart=unless-stopped", self.name(slot))
        if not self.running("gateway"):
            self.compose("up", "-d", "--no-deps", "gateway")
        self.restore_route(source)
        context = self.state["maintenance_context"]
        self.state = original["state"]
        self.save(last_maintenance=context)
        log("MAINTENANCE CANCELLED BEFORE MIGRATION: old service resumed; existing requests preserved")

    def redact_maintenance(self, text):
        config = json.loads(self.compose_path.read_text(encoding="utf-8"))
        values = []
        for service in config["services"].values():
            for key, value in service.get("environment", {}).items():
                if value and re.search(r"secret|password|token|key|dsn|conn_string", key, re.I):
                    values.append(str(value))
                    match = re.match(r"[^:]+:(.*)@tcp\(", str(value))
                    if match and match[1]:
                        values.append(match[1])
        for value in sorted(values, key=len, reverse=True):
            text = text.replace(value, "[REDACTED]")
        text = re.sub(r"(?i)(password|secret|token|api_key)(\s*[=:]\s*)[^\s,;]+", r"\1\2[REDACTED]", text)
        return re.sub(r"://[^\s/@]+:[^\s/@]+@", "://[REDACTED]@", text)

    def capture_backup(self, target, args, timeout=1800):
        partial = target.with_suffix(target.suffix + ".partial")
        with open(partial, "wb") as output:
            os.chmod(partial, 0o600)
            result = subprocess.run(args, stdout=output, stderr=subprocess.PIPE, timeout=timeout)
            output.flush()
            os.fsync(output.fileno())
        if result.returncode or partial.stat().st_size == 0:
            message = self.redact_maintenance(result.stderr.decode("utf-8", errors="replace"))
            atomic_json(target.with_suffix(".error.json"), {"exit_code": result.returncode, "error": message})
            raise DeployError("maintenance backup failed; private diagnostic saved; migration was not started")
        os.replace(partial, target)

    @staticmethod
    def file_digest(path):
        digest = hashlib.sha256()
        with open(path, "rb") as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
        return digest.hexdigest()

    def backup_maintenance(self):
        context = self.maintenance_directory()
        config = self.original_maintenance()["config"]
        env = config["services"]["worker"]["environment"]
        files = [context / "original.json"]
        seen = set()
        strategy = self.state.get("maintenance_backup_strategy") or "stopped_full"
        if strategy == "zfs_snapshot":
            # Apps are already quiesced and stopped; the snapshot is the
            # stopped boundary. Names derive from the context, so recover can
            # never confuse one maintenance snapshot with another.
            snapshot = self.snapshot_database("maint-" + context.name[len("maintenance-"):])
            atomic_json(context / "snapshot.json", snapshot)
            files.append(context / "snapshot.json")
            log(f"MAINTENANCE SNAPSHOT: {snapshot['dataset']}@eflow-{snapshot['name']} frozen {snapshot['frozen_seconds']}s")
            seen.add("snapshot")
        for key in (() if strategy == "zfs_snapshot" else ("SQL_DSN", "LOG_SQL_DSN")):
            dsn = env.get(key)
            if not dsn or dsn in seen:
                continue
            seen.add(dsn)
            match = re.fullmatch(r"([^:]+):(.*)@tcp\(([^)]+)\)/([^?]+)(?:\?(.*))?", dsn)
            if not match:
                raise DeployError("backup requires a supported TCP MySQL DSN; migration was not started")
            user, password, address, database, query = match.groups()
            host, port = address.rsplit(":", 1) if ":" in address else (address, "3306")
            args = ["mysqldump", "--host=" + host.strip("[]"), "--port=" + port, "--user=" + user,
                    "--default-character-set=utf8mb4", "--single-transaction", "--quick", "--skip-lock-tables",
                    "--no-tablespaces", "--set-gtid-purged=OFF", "--hex-blob", "--routines", "--events", "--triggers",
                    "--init-command=SET SESSION MAX_EXECUTION_TIME=0"]
            tls = urllib.parse.parse_qs(query or "").get("tls", ["false"])[0]
            if tls not in ("true", "false", "skip-verify", "preferred"):
                raise DeployError("custom MySQL TLS requires an explicit backup client configuration")
            args.append("--ssl-mode=" + {"true": "VERIFY_IDENTITY", "false": "DISABLED", "skip-verify": "REQUIRED", "preferred": "PREFERRED"}[tls])
            args.append(urllib.parse.unquote(database))
            client = {"services": {"backup": {"image": BACKUP_IMAGE, "entrypoint": args,
                       "environment": {"MYSQL_PWD": password}, "networks": list(config["services"]["worker"]["networks"])}},
                      "networks": config["networks"]}
            client_path = context / (key.lower() + "-client.json")
            atomic_json(client_path, client)
            target = context / (key.lower() + ".sql")
            self.capture_backup(target, ["docker", "compose", "-p", PREFIX, "-f", str(client_path), "run", "--rm", "--no-deps", "backup"])
            with open(target, "rb") as stream:
                stream.seek(max(0, target.stat().st_size - 1024))
                if b"-- Dump completed on" not in stream.read():
                    raise DeployError("database backup has no completion marker; migration was not started")
            files.append(target)
        if not seen:
            raise DeployError("database backup configuration is missing")
        target = context / "data.tar.gz"
        self.capture_backup(target, ["docker", "compose", "-p", PREFIX, "-f", str(self.compose_path),
                            "run", "--rm", "--no-deps", "--entrypoint", "tar", "migrate", "-czf", "-", "-C", "/data", "."])
        files.append(target)
        manifest = {"strategy": strategy,
                    "files": [{"file": p.name, "bytes": p.stat().st_size, "sha256": self.file_digest(p)} for p in files]}
        atomic_json(context / "backup.json", manifest)
        self.save(maintenance_backup=self.file_digest(context / "backup.json"))
        log("MAINTENANCE BACKUP COMPLETE: database, data and previous deployment snapshots verified")

    def verify_maintenance_backup(self):
        context = self.maintenance_directory()
        path = context / "backup.json"
        if not path.exists() or self.file_digest(path) != self.state.get("maintenance_backup"):
            raise DeployError("maintenance backup manifest is missing or changed")
        manifest = json.loads(path.read_text(encoding="utf-8"))
        strategy = manifest.get("strategy", "stopped_full")
        if strategy != (self.state.get("maintenance_backup_strategy") or "stopped_full"):
            raise DeployError("maintenance backup strategy changed")
        required = {"original.json", "data.tar.gz", "snapshot.json" if strategy == "zfs_snapshot" else "sql_dsn.sql"}
        for entry in manifest["files"]:
            if Path(entry["file"]).name != entry["file"]:
                raise DeployError("invalid backup path")
            target = context / entry["file"]
            if not target.exists() or target.stat().st_size != entry["bytes"] or self.file_digest(target) != entry["sha256"]:
                raise DeployError("maintenance backup contents are missing or changed")
            required.discard(entry["file"])
        if required:
            raise DeployError("maintenance backup is incomplete")
        if strategy == "zfs_snapshot":
            recorded = json.loads((context / "snapshot.json").read_text(encoding="utf-8"))
            if self.snapshot_guard("verify", recorded["name"]).get("guid") != recorded["guid"]:
                raise DeployError("maintenance snapshot is missing or was replaced")

    def migration_exists(self, name):
        return bool(run("docker", "ps", "-aq", "--filter", "name=^/" + name + "$").strip())

    def run_maintenance_migration(self):
        context = self.maintenance_directory()
        name = self.state.get("migration_container")
        if not name or self.state.get("migration_failed"):
            name = PREFIX + "-migrate-" + uuid.uuid4().hex[:12]
            self.save(migration_container=name, migration_failed=False)
        if not self.migration_exists(name):
            # A unique retained container closes the SSH/client-timeout window:
            # recover observes the same process instead of starting a second one.
            self.compose("run", "-d", "--no-deps", "--name", name, "migrate", timeout=90)
        deadline = time.monotonic() + 600
        while True:
            container = self.inspect(name)
            if container["Image"] != self.state["maintenance_image"]:
                raise DeployError("migration container image mismatch")
            status = container["State"]
            if status.get("Status") == "created":
                run("docker", "start", name)
                time.sleep(1)
                continue
            if not status["Running"]:
                break
            if time.monotonic() >= deadline:
                raise DeployError("migration still running; run recover to observe it, do not start another migration")
            time.sleep(1)
        logs = subprocess.run(["docker", "logs", "--tail", "2000", name], capture_output=True, timeout=30)
        if logs.returncode:
            raise DeployError("migration log capture failed; container retained for recover")
        output = (logs.stdout + logs.stderr).decode("utf-8", errors="replace")
        atomic_json(context / (name + ".json"), {"container": name, "image": container["Image"],
                    "exit_code": status["ExitCode"], "log": self.redact_maintenance(output)})
        if status["ExitCode"] != 0 or status.get("Status") != "exited" or status.get("OOMKilled"):
            self.save(migration_failed=True)
            raise DeployError(f"migration failed (exit {status['ExitCode']}); diagnostic retained in {context.name}; fix cause then run recover")
        self.save(phase="starting", migration_failed=False)

    def continue_maintenance(self):
        if not self.state.get("migration_started"):
            raise DeployError("migration intent is missing; cannot start candidate")
        self.verify_maintenance_backup()
        source = self.state["maintenance_source"]
        immutable = self.state["maintenance_image"]
        config = json.loads(self.compose_path.read_text(encoding="utf-8"))
        for slot in ("blue", "green", "worker", "migrate"):
            config["services"][slot]["image"] = immutable
        atomic_json(self.compose_path, config)
        if self.state["phase"] == "migrating":
            log("MAINTENANCE: running or observing retained migration role")
            self.run_maintenance_migration()
        self.compose("up", "-d", "--no-deps", "worker", source)
        for slot in ("worker", source):
            run("docker", "update", "--restart=unless-stopped", self.name(slot))
        self.ready("worker")
        self.ready(source)
        for path in ("status", "pricing"):
            payload = json.loads(run("docker", "exec", self.name(source), "wget", "-q", "-T", "5", "-O", "-", "http://127.0.0.1:3000/api/" + path))
            if not payload.get("success"):
                raise DeployError("candidate business API failed; maintenance remains active")
        self.save(phase="maintenance_switching")
        self.compose("up", "-d", "--no-deps", "gateway")
        self.restore_route(source)
        self.save(phase="stable", active=source, previous=None,
                  pending_cleanup=None, worker_image=immutable,
                  last_maintenance=self.state["maintenance_context"], maintenance_context=None,
                  maintenance_source=None, maintenance_image=None, migration_started=None,
                  migration_container=None, migration_failed=None, maintenance_backup=None,
                  maintenance_backup_strategy=None, contract=CONTRACT, maintenance_step="restored")
        log(f"MAINTENANCE COMPLETE: {source} is serving the migrated billing data")

    def require_stable(self):
        if self.state.get("phase") != "stable":
            raise DeployError("rolling deployment is not stable/initialized; use status and recover, or initialize during maintenance")

    def rollback(self):
        self.require_stable()
        target = self.state.get("previous")
        if target not in ("blue", "green") or target == self.state["active"]:
            raise DeployError("no previous slot available")
        previous_release = self.state.get("previous_release")
        if self.state.get("schema") == 2 and not (previous_release and previous_release.get("revision")):
            raise DeployError("previous slot source identity is unknown; refusing an untraceable rollback")
        current_release = {"revision": self.state.get("release_revision"),
                           "fingerprints": self.state.get("release_fingerprints"),
                           "operation": self.state.get("last_release_operation"),
                           "worker_image": self.state.get("worker_image"),
                           "worker_revision": self.state.get("worker_revision"),
                           "worker_fingerprint": self.state.get("worker_fingerprint")}
        self.ready("worker")
        run("docker", "start", self.name(target))
        deadline = time.monotonic() + 90
        while True:
            try:
                self.control(target, "status")
                break
            except (DeployError, subprocess.TimeoutExpired, ValueError):
                if time.monotonic() >= deadline:
                    raise DeployError("previous instance did not start") from None
                time.sleep(1)
        self.control(target, "resume", True)
        self.ready(target)
        run("docker", "update", "--restart=unless-stopped", self.name(target))
        release_state = ({"release_revision": previous_release["revision"],
                          "release_fingerprints": previous_release.get("fingerprints"),
                          "last_release_operation": previous_release.get("operation"),
                          "release_operation": previous_release.get("operation"), "previous_release": current_release}
                         if previous_release else None)
        self.switch(target, release_state)
        self.cleanup()
        # A release that also handed off the worker rolls the worker back too,
        # so web and worker always come from the same release.
        worker_image = (previous_release or {}).get("worker_image")
        if worker_image and worker_image != self.state.get("worker_image"):
            self.handoff_worker(worker_image)
            self.save(worker_revision=previous_release.get("worker_revision"),
                      worker_fingerprint=previous_release.get("worker_fingerprint"))

    def recover(self):
        if self.state.get("phase") == "stable":
            self.cleanup()
            return
        if self.state.get("phase") == "worker_handoff":
            # Interrupted between stopping the old worker and confirming the new
            # one: always return to the image that was serving before.
            self.restore_worker(self.state["worker_handoff"]["from"])
            return
        if self.state.get("maintenance_context"):
            if self.state.get("phase") == "maintenance" and not self.state.get("migration_started"):
                self.resume_before_migration()
                return
            if self.state.get("phase") in ("migrating", "starting", "maintenance_switching") and self.state.get("migration_started"):
                self.continue_maintenance()
                return
            raise DeployError("inconsistent maintenance state; inspect retained evidence before proceeding")
        if self.state.get("phase") not in ("preparing", "switching"):
            raise DeployError("initial migration recovery requires the saved legacy Compose and database backup; see documentation")
        # active is committed only after the new route was verified. Restoring
        # the committed slot is safe even after an interrupted nginx reload.
        source = self.state["active"]
        self.control(source, "resume", True)
        self.ready(source)
        self.reload(source)
        self.verify_route(source)
        other = "green" if source == "blue" else "blue"
        self.save(phase="stable", pending_cleanup=other)
        self.cleanup()

    def initialize(self, archive, image, backup_confirmed):
        if not backup_confirmed:
            raise DeployError("initial migration requires an existing database backup and maintenance window")
        if self.state:
            raise DeployError("state already exists; inspect it instead of overwriting a deployment")
        source = json.loads(run("docker", "compose", "config", "--format", "json"))
        old_id = run("docker", "compose", "ps", "-q", "new-api").strip()
        if not old_id or "\n" in old_id:
            raise DeployError("expected one running legacy new-api service")
        old = self.inspect(old_id)
        networks = list(old["NetworkSettings"]["Networks"])
        if len(networks) != 1:
            raise DeployError("initial migration currently requires exactly one Docker network")
        network = networks[0]
        template = source["services"]["new-api"]
        if template.get("entrypoint") or source.get("secrets") or source.get("configs"):
            raise DeployError("custom entrypoint/secrets/config mounts require an explicit migration configuration")
        if not any(mount.get("Destination") == "/data" and mount.get("RW") for mount in old["Mounts"]):
            raise DeployError("a shared writable /data mount is required for the worker lock")
        env = dict(template.get("environment", {}))
        if not env.get("SESSION_SECRET") or not env.get("REDIS_CONN_STRING"):
            raise DeployError("shared SESSION_SECRET and Redis must already be configured")
        if str(env.get("BATCH_UPDATE_ENABLED", "false")).lower() == "true":
            raise DeployError("disable batch quota updates in a maintenance release before migration")
        ports = old["HostConfig"]["PortBindings"]
        if ports != {"3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "3000"}]}:
            raise DeployError("expected the existing loopback 127.0.0.1:3000 binding")
        immutable = self.load_image(archive, image)
        run("docker", "pull", GATEWAY_IMAGE, timeout=300)
        gateway_image = json.loads(run("docker", "image", "inspect", GATEWAY_IMAGE))[0]["Id"]
        source["services"]["new-api"]["image"] = old["Image"]
        atomic_json(self.directory / "legacy-compose.json", source)
        atomic_json(self.directory / "legacy-container.json", old)
        services = {}
        for slot in ("blue", "green", "worker", "migrate"):
            service = copy.deepcopy(template)
            for key in ("build", "ports", "depends_on", "links", "profiles", "network_mode", "hostname", "entrypoint"):
                service.pop(key, None)
            service.update(image=immutable, container_name=self.name(slot), restart="unless-stopped",
                           networks={"runtime": {"aliases": [self.name(slot)]}})
            role = "web" if slot in ("blue", "green") else slot
            service["environment"] = dict(env, DEPLOYMENT_ROLE=role, NODE_TYPE="slave" if role == "web" else "master",
                                          NODE_NAME=self.name(slot), PORT="3000", SQL_MAX_OPEN_CONNS="20",
                                          SQL_MAX_IDLE_CONNS="5", BATCH_UPDATE_ENABLED="false")
            service["environment"].pop("VERSION", None)
            service["healthcheck"] = {"test": ["CMD", "wget", "-q", "-T", "5", "-O", "-", "http://127.0.0.1:3001/ready"],
                                      "interval": "10s", "timeout": "6s", "retries": 3, "start_period": "30s"}
            if slot == "migrate":
                service["restart"] = "no"
                service["healthcheck"] = {"disable": True}
            services[slot] = service
        services["gateway"] = {"image": gateway_image, "container_name": self.name("gateway"), "restart": "unless-stopped",
                               "ports": ["127.0.0.1:3000:3000"], "networks": {"runtime": {"aliases": ["new-api"]}},
                               "volumes": [{"type": "bind", "source": str(self.directory / "nginx"),
                                            "target": "/etc/nginx", "read_only": True}]}
        config = {"services": services, "networks": {"runtime": {"external": True, "name": network}}}
        if source.get("volumes"):
            config["volumes"] = {key: {"external": True, "name": value.get("name", key)} for key, value in source["volumes"].items()}
        atomic_json(self.compose_path, config)
        self.compose("config", "--quiet")
        self.write_route("blue")
        self.save(phase="initializing", active=None, worker_image=immutable, legacy_id=old_id, network=network,
                  contract=CONTRACT)
        log("INITIAL MIGRATION: maintenance window starts; legacy application is stopping")
        run("docker", "compose", "stop", "new-api", timeout=300)
        # Prevent a daemon restart from reviving a second master.
        run("docker", "update", "--restart=no", old_id)
        run("docker", "network", "disconnect", network, old_id)
        self.save(phase="migrating")
        self.compose("run", "--rm", "--no-deps", "migrate", timeout=600)
        self.save(phase="starting")
        self.compose("up", "-d", "--no-deps", "worker", "blue")
        self.ready("worker")
        self.ready("blue")
        self.compose("up", "-d", "--no-deps", "gateway")
        self.verify_route("blue")
        self.save(phase="stable", active="blue", previous=None, pending_cleanup=None)
        log("INITIAL MIGRATION COMPLETE: gateway owns both 127.0.0.1:3000 and Docker new-api:3000")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["rollback", "cleanup", "status", "retire-rollback"])
    parser.add_argument("--expected-revision")
    parser.add_argument("--expected-image")
    parser.add_argument("--root", default=".")
    parser.add_argument("--wait", type=int, default=120)
    args = parser.parse_args()
    os.umask(0o077)
    deployment = Deployment(args.root, args.wait)
    os.chdir(deployment.root)
    if args.action == "status":
        print(json.dumps(deployment.state, indent=2))
        if deployment.compose_path.exists():
            print(deployment.compose("ps", "-a"))
        return
    # Linux host lock remains held for the whole operation, independent of SSH.
    import fcntl
    with open(deployment.directory / "deploy.lock", "w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise DeployError("another deployment is running") from None
        deployment.state = json.loads(deployment.state_path.read_text(encoding="utf-8")) if deployment.state_path.exists() else {}
        if args.action == "rollback":
            deployment.rollback()
        elif args.action == "retire-rollback":
            deployment.retire_rollback(args.expected_revision, args.expected_image)
        else:
            deployment.require_stable()
            deployment.cleanup()


if __name__ == "__main__":
    try:
        main()
    except (DeployError, subprocess.TimeoutExpired) as error:
        log(str(error))
        sys.exit(1)
