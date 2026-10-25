"""Explicit opt-in Docker integration test; creates and removes its own stack.

Build Dockerfile.dev as new-api-rolling-test:20260907 before running. Port 3000
must be free. No production connections or credentials are used.
"""
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import socket
import tempfile
import time
import urllib.request
import uuid

scripts = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("rolling", scripts / "rolling-deploy.py")
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)


def main():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 3000))
    root = Path(tempfile.mkdtemp(prefix="new-api-rolling-integration-"))
    suffix = uuid.uuid4().hex[:8]
    project = "rolling-it-" + suffix
    rolling.PREFIX = project
    mysql = project + "-mysql"
    old = project + "-legacy"
    network = project + "-network"
    tag = "new-api-eflow:release-integration-" + suffix
    archive = root / "release.tar"
    compose_path = root / "compose.json"
    deployment = rolling.Deployment(root, wait=0)
    password = "RollingTest123"
    environment = {"SQL_DSN": f"root:{password}@tcp(mysql:3306)/rolling_test?charset=utf8mb4&parseTime=True&loc=Local",
                   "REDIS_CONN_STRING": "redis://redis:6379", "SESSION_SECRET": "rolling-integration-session-secret",
                   "BATCH_UPDATE_ENABLED": "false", "CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED": "false",
                   "SYNC_FREQUENCY": "1", "SQL_MAX_OPEN_CONNS": "20", "SQL_MAX_IDLE_CONNS": "5"}
    base = {"name": project + "-legacy-stack", "services": {
        "mysql": {"image": "mysql:8.4.8", "container_name": mysql,
                  "environment": {"MYSQL_ROOT_PASSWORD": password, "MYSQL_DATABASE": "rolling_test"},
                  "networks": ["runtime"], "volumes": ["mysql-test:/var/lib/mysql"],
                  "healthcheck": {"test": ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1"], "interval": "2s", "retries": 60}},
        "redis": {"image": "redis:7-alpine", "networks": ["runtime"]},
        "upstream": {"image": "node:20-bookworm", "command": ["node", "/upstream.cjs"], "networks": ["runtime"],
                     "ports": ["127.0.0.1:31789:8080"],
                     "volumes": [{"type": "bind", "source": str(scripts / "rolling-test-upstream.cjs"), "target": "/upstream.cjs", "read_only": True}]},
        "new-api": {"image": "new-api-rolling-test:20260907", "container_name": old, "restart": "unless-stopped",
                    "environment": environment, "ports": ["127.0.0.1:3000:3000"], "networks": {"runtime": {"aliases": ["new-api"]}},
                    "volumes": [{"type": "bind", "source": str(root / "data"), "target": "/data"}],
                    "depends_on": {"mysql": {"condition": "service_healthy"}, "redis": {"condition": "service_started"}}}},
        "networks": {"runtime": {"name": network}}, "volumes": {"mysql-test": {}}}
    (root / "data").mkdir()
    rolling.atomic_json(compose_path, base)
    cookies = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(cookies))

    def api(path, body=None, method=None):
        request = urllib.request.Request("http://127.0.0.1:3000" + path,
                                         data=json.dumps(body).encode() if body is not None else None,
                                         headers={"Content-Type": "application/json", "New-Api-User": "1"}, method=method)
        with client.open(request, timeout=15) as response:
            payload = json.load(response)
            if not payload.get("success"):
                raise AssertionError(f"{path}: {payload}")
            return payload

    def sql(statement):
        return rolling.run("docker", "exec", "-e", "MYSQL_PWD=" + password, mysql,
                           "mysql", "-uroot", "--batch", "--skip-column-names", "rolling_test", "-e", statement).strip()

    initial_cwd = Path.cwd()
    previous_compose = os.environ.get("COMPOSE_FILE")
    os.environ["COMPOSE_FILE"] = str(compose_path)
    stream = None
    try:
        os.chdir(root)
        rolling.run("docker", "compose", "up", "-d", timeout=180)
        deadline = time.monotonic() + 120
        while True:
            try:
                api("/api/status")
                break
            except OSError:
                if time.monotonic() > deadline:
                    raise
                time.sleep(1)
        api("/api/setup", {"username": "root", "password": password, "confirmPassword": password})
        api("/api/user/login", {"username": "root", "password": password})
        api("/api/channel/", {"mode": "single", "channel": {"type": 1, "name": "isolated-upstream", "key": "test-only",
                                                             "base_url": "http://upstream:8080", "models": "gpt-4o-mini", "group": "default", "status": 1}})
        api("/api/token/", {"name": "rolling-test", "unlimited_quota": True, "expired_time": -1})
        token = sql("SELECT `key` FROM tokens LIMIT 1")
        before = int(sql("SELECT quota FROM users WHERE id=1"))
        rolling.run("docker", "tag", "new-api-rolling-test:20260907", tag)
        rolling.run("docker", "save", "-o", str(archive), tag, timeout=180)
        deployment.initialize(archive, tag, True)
        api("/api/user/self")  # Shared session survives legacy -> blue.
        assert not deployment.inspect(old)["State"]["Running"]
        assert deployment.control("worker")["role"] == "worker"
        assert deployment.control("blue")["role"] == "web"
        worker_id = deployment.inspect(deployment.name("worker"))["Id"]
        try:
            deployment.compose("run", "--rm", "--no-deps", "worker", timeout=20)
            raise AssertionError("second worker acquired the shared lock")
        except rolling.DeployError:
            pass
        deployment.ready("worker")
        request = urllib.request.Request("http://127.0.0.1:3000/v1/chat/completions", data=json.dumps({
            "model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hello"}], "stream": True,
            "stream_options": {"include_usage": True}}).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer sk-" + token})
        stream = client.open(request, timeout=90)
        first = stream.readline()
        assert b"hello" in first, first
        assert stream.headers.get("X-New-Api-Instance") == deployment.name("blue")
        deployment.deploy(archive, tag)
        assert deployment.state["active"] == "green"
        assert deployment.state["pending_cleanup"] == "blue"
        assert deployment.running("blue")
        assert deployment.inspect(deployment.name("worker"))["Id"] == worker_id
        internal_instance = rolling.run("docker", "exec", project + "-legacy-stack-upstream-1", "node", "-e",
                                        "fetch('http://new-api:3000/api/status').then(r=>console.log(r.headers.get('X-New-Api-Instance')))").strip()
        assert internal_instance == deployment.name("green"), internal_instance
        api("/api/user/self")  # Existing cookie also survives blue -> green.
        with client.open("http://127.0.0.1:31789/release", timeout=5) as release:
            release.read()
        remaining = stream.read()
        assert b"[DONE]" in remaining, remaining
        stream.close()
        stream = None
        deployment.wait = 15
        deployment.cleanup()
        assert deployment.state["pending_cleanup"] is None
        assert not deployment.running("blue")
        after = int(sql("SELECT quota FROM users WHERE id=1"))
        log_quota = int(sql("SELECT COALESCE(SUM(quota),0) FROM logs WHERE type=2 AND user_id=1"))
        assert before - after == log_quota and log_quota > 0, (before, after, log_quota)
        assert sql("SELECT COUNT(*) FROM logs WHERE type=2 AND user_id=1") == "1"
        deployment.rollback()
        assert deployment.state["active"] == "blue"
        deployment.verify_route("blue")
        # Candidate dependency failure must leave the current route untouched.
        config = json.loads(deployment.compose_path.read_text(encoding="utf-8"))
        config["services"]["green"]["environment"]["SQL_DSN"] = "root:invalid@tcp(127.0.0.1:1)/invalid"
        rolling.atomic_json(deployment.compose_path, config)
        real_ready = deployment.ready
        deployment.ready = lambda slot, timeout=90: real_ready(slot, timeout=5 if slot == "green" else timeout)
        try:
            deployment.deploy(archive, tag)
            raise AssertionError("unhealthy candidate accepted")
        except rolling.DeployError:
            pass
        assert deployment.state["active"] == "blue"
        deployment.verify_route("blue")
        rolling.log("PASS: initial migration, SSE cutover, session continuity, exact single billing, no duplicate worker, drain, rollback, dependency failure")
    finally:
        if stream is not None:
            try:
                with client.open("http://127.0.0.1:31789/release", timeout=3) as release:
                    release.read()
            except OSError:
                pass
            stream.close()
        # Only the two unique test Compose projects are stopped; no prune.
        if deployment.compose_path.exists():
            deployment.compose("down", timeout=180)
        rolling.run("docker", "compose", "-f", str(compose_path), "down", "--volumes", timeout=180)
        os.chdir(initial_cwd)
        if previous_compose is None:
            os.environ.pop("COMPOSE_FILE", None)
        else:
            os.environ["COMPOSE_FILE"] = previous_compose
        rolling.log("Isolated containers stopped; evidence directory: " + str(root))


if __name__ == "__main__":
    main()
