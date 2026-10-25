import importlib.util
import json
import re
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("rolling", Path(__file__).with_name("rolling-deploy.py"))
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)


class RollingTests(unittest.TestCase):
    def test_retire_rollback_requires_exact_active_identity_and_stopped_previous(self):
        self.deploy.save(release_revision='bridge', previous_release={'revision': 'old'})
        self.deploy.inspect = Mock(return_value={'Image': 'sha256:bridge'})
        self.deploy.cleanup = Mock()
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.ready = Mock()
        self.deploy.verify_route = Mock()
        self.deploy.running = Mock(return_value=True)
        with self.assertRaises(rolling.DeployError):
            self.deploy.retire_rollback('wrong', 'sha256:bridge')
        with self.assertRaises(rolling.DeployError):
            self.deploy.retire_rollback('bridge', 'sha256:wrong')
        with self.assertRaises(rolling.DeployError):
            self.deploy.retire_rollback('bridge', 'sha256:bridge')
        self.assertEqual(self.deploy.state['previous'], 'green')
        self.deploy.running.return_value = False
        self.deploy.retire_rollback('bridge', 'sha256:bridge')
        self.assertIsNone(self.deploy.state['previous'])
        self.assertIsNone(self.deploy.state['previous_release'])
        self.assertEqual(self.deploy.state['active'], 'blue')
        self.assertEqual(self.deploy.state['worker_image'], 'sha256:worker')
        archived = list(self.deploy.directory.glob('retired-rollback-*.json'))
        self.assertEqual(len(archived), 1)
        self.assertEqual(json.loads(archived[0].read_text())['previous'], 'green')
        with self.assertRaises(rolling.DeployError):
            self.deploy.rollback()

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.deploy = rolling.Deployment(self.directory.name, wait=0)
        self.deploy.verify_auth_continuity = Mock()
        self.deploy.retain_assets = Mock()
        self.deploy.save(phase="stable", active="blue", worker_image="sha256:worker", previous="green")

    def test_switch_commits_only_after_route_verification(self):
        observed = []
        self.deploy.reload = Mock(side_effect=lambda target: observed.append((target, self.deploy.state["active"])))
        self.deploy.verify_route = Mock(side_effect=lambda target: observed.append((target, self.deploy.state["active"])))
        self.deploy.switch("green")
        self.assertEqual(observed, [("green", "blue"), ("green", "blue")])
        self.assertEqual(self.deploy.state["active"], "green")
        self.assertEqual(self.deploy.state["pending_cleanup"], "blue")
        self.assertEqual(json.loads(self.deploy.state_path.read_text())["active"], "green")

    def test_failed_route_verification_restores_old_route(self):
        self.deploy.reload = Mock()
        self.deploy.control = Mock()
        self.deploy.ready = Mock()
        self.deploy.verify_route = Mock(side_effect=[rolling.DeployError("candidate failed"), None])
        with self.assertRaises(rolling.DeployError):
            self.deploy.switch("green")
        self.assertEqual(self.deploy.reload.call_args_list[-1].args, ("blue",))
        self.assertEqual(self.deploy.state["active"], "blue")
        self.assertEqual(self.deploy.state["phase"], "stable")

    def test_asset_archive_failure_does_not_start_cutover(self):
        self.deploy.retain_assets.side_effect = RuntimeError('asset collision')
        self.deploy.reload = Mock()
        with self.assertRaises(RuntimeError):
            self.deploy.switch('green')
        self.deploy.reload.assert_not_called()
        self.assertEqual(self.deploy.state['active'], 'blue')
        self.assertEqual(self.deploy.state['phase'], 'stable')

    def test_failed_rollback_keeps_switch_intent_for_recovery(self):
        self.deploy.reload = Mock(side_effect=rolling.DeployError("invalid config"))
        self.deploy.control = Mock()
        self.deploy.ready = Mock()
        with self.assertRaises(rolling.DeployError):
            self.deploy.switch("green")
        self.assertEqual(self.deploy.state["phase"], "switching")
        self.assertEqual(self.deploy.state["active"], "blue")

    def test_long_gateway_connection_keeps_old_instance_alive(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=True)
        self.deploy.control = Mock()
        with patch.object(rolling, "run") as command:
            self.deploy.cleanup()
        self.deploy.control.assert_not_called()
        command.assert_not_called()
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")

    def test_pending_settlement_is_not_killed(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.control = Mock(return_value={"active": 0, "pending": 1})
        with patch.object(rolling, "run") as command:
            self.deploy.cleanup()
        command.assert_not_called()
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")

    def test_successful_cleanup_flushes_before_sigterm_without_kill_timeout(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.control = Mock(return_value={"active": 0, "pending": 0})
        with patch.object(rolling, "run", return_value="0\n") as command:
            self.deploy.cleanup()
        self.assertEqual(self.deploy.control.call_args_list[-1].args, ("green", "quiesce", True))
        self.assertEqual(command.call_args_list[1].args, ("docker", "kill", "--signal=TERM", self.deploy.name("green")))
        self.assertIsNone(self.deploy.state["pending_cleanup"])

    def test_failed_flush_preserves_old_instance(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.control = Mock(side_effect=[{"active": 0, "pending": 0}, rolling.DeployError("flush failed")])
        with patch.object(rolling, "run") as command:
            with self.assertRaises(rolling.DeployError):
                self.deploy.cleanup()
        command.assert_not_called()

    def test_unexpected_exit_is_not_reported_as_success(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.control = Mock(return_value={"active": 0, "pending": 0})
        with patch.object(rolling, "run", return_value="137\n"):
            with self.assertRaises(rolling.DeployError):
                self.deploy.cleanup()
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")

    def test_candidate_failure_does_not_switch(self):
        self.deploy.cleanup = Mock()
        self.deploy.ready = Mock(side_effect=[{}, {}, rolling.DeployError("not ready")])
        self.deploy.verify_route = Mock()
        self.deploy.load_image = Mock(return_value="sha256:candidate")
        self.deploy.configure_image = Mock()
        self.deploy.compose = Mock()
        self.deploy.switch = Mock()
        with self.assertRaises(rolling.DeployError):
            self.deploy.deploy("archive", "tag")
        self.deploy.switch.assert_not_called()
        self.assertEqual(self.deploy.state["active"], "blue")
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")
        self.assertIsNone(self.deploy.state["previous"])

    def handoff_mocks(self, ready=None):
        self.deploy.load_image = Mock(return_value="sha256:new")
        self.deploy.inspect = Mock(return_value={"Image": "sha256:worker"})
        self.deploy.ready = Mock(side_effect=ready)
        self.deploy.control = Mock(return_value={"active": 0, "pending": 0})
        self.deploy.stop_quiesced = Mock()
        self.deploy.configure_image = Mock()
        self.deploy.compose = Mock()
        self.deploy.running = Mock(return_value=False)

    def test_worker_handoff_stops_old_before_starting_new(self):
        self.handoff_mocks()
        calls = []
        self.deploy.stop_quiesced.side_effect = lambda slot: calls.append(("stop", slot))
        self.deploy.configure_image.side_effect = lambda slot, image: calls.append(("image", slot, image))
        with patch.object(rolling, "run"):
            self.deploy.handoff_worker("sha256:new")
        self.assertEqual(calls, [("stop", "worker"), ("image", "worker", "sha256:new")])
        posted = [c.args[1] for c in self.deploy.control.call_args_list if c.args[2:] == (True,)]
        self.assertEqual(posted[:2], ["drain", "quiesce"])
        self.assertEqual((self.deploy.state["phase"], self.deploy.state["worker_image"]), ("stable", "sha256:new"))

    def test_failed_new_worker_restores_old_image(self):
        self.handoff_mocks(ready=[{}, rolling.DeployError("not ready"), {}])
        with patch.object(rolling, "run", return_value="container-id\n"):
            with self.assertRaises(rolling.DeployError):
                self.deploy.handoff_worker("sha256:new")
        images = [c.args[1] for c in self.deploy.configure_image.call_args_list]
        self.assertEqual(images, ["sha256:new", "sha256:worker"])
        self.assertEqual((self.deploy.state["phase"], self.deploy.state["worker_image"]), ("stable", "sha256:worker"))

    def test_worker_drain_timeout_keeps_old_worker(self):
        self.handoff_mocks()
        self.deploy.control.return_value = {"active": 0, "pending": 2}
        with patch.object(rolling, "run"):
            with self.assertRaises(rolling.DeployError):
                self.deploy.handoff_worker("sha256:new")
        self.deploy.stop_quiesced.assert_not_called()
        self.assertIn(("worker", "resume", True), [c.args for c in self.deploy.control.call_args_list])
        self.assertEqual(self.deploy.state["phase"], "stable")

    def test_interrupted_handoff_recovers_previous_worker(self):
        self.handoff_mocks()
        self.deploy.save(phase="worker_handoff", worker_handoff={"from": "sha256:worker", "to": "sha256:new"})
        with patch.object(rolling, "run", return_value=""):
            self.deploy.recover()
        self.deploy.configure_image.assert_called_once_with("worker", "sha256:worker")
        self.assertEqual(self.deploy.state["phase"], "stable")

    def test_rollback_returns_worker_with_web(self):
        self.deploy.save(schema=2, release_revision="new", worker_image="sha256:new", previous="green",
                         previous_release={"revision": "old", "worker_image": "sha256:worker",
                                           "worker_revision": "old", "worker_fingerprint": "fp-old"})
        self.deploy.ready = Mock()
        self.deploy.control = Mock()
        self.deploy.switch = Mock(side_effect=lambda target, state: self.deploy.save(active=target, **state))
        self.deploy.cleanup = Mock()
        self.deploy.handoff_worker = Mock(side_effect=lambda image: self.deploy.save(worker_image=image))
        with patch.object(rolling, "run"):
            self.deploy.rollback()
        self.deploy.handoff_worker.assert_called_once_with("sha256:worker")
        self.assertEqual((self.deploy.state["worker_revision"], self.deploy.state["release_revision"]), ("old", "old"))

    def test_busy_slot_cannot_be_recreated(self):
        self.deploy.save(pending_cleanup="green")
        self.deploy.cleanup = Mock()
        self.deploy.load_image = Mock()
        with self.assertRaises(rolling.DeployError):
            self.deploy.deploy("archive", "tag")
        self.deploy.load_image.assert_not_called()

    def test_recovery_uses_committed_source_after_interrupted_switch(self):
        self.deploy.save(phase="switching", switching_from="blue", switching_to="green")
        self.deploy.control = Mock()
        self.deploy.ready = Mock()
        self.deploy.reload = Mock()
        self.deploy.verify_route = Mock()
        self.deploy.cleanup = Mock()
        self.deploy.recover()
        self.deploy.reload.assert_called_once_with("blue")
        self.assertEqual(self.deploy.state["active"], "blue")
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")

    def test_contract_matches_runtime_and_rejects_previous_worker(self):
        lifecycle = Path(__file__).resolve().parent.parent / "internal" / "deployment" / "lifecycle.go"
        self.assertIn(f'const Contract = "{rolling.CONTRACT}"', lifecycle.read_text(encoding="utf-8"))
        self.deploy.control = Mock(return_value={
            "contract": "eflow-rolling-v7", "role": "worker", "ready": True,
            "instance": self.deploy.name("worker"),
        })
        with self.assertRaises(rolling.DeployError):
            self.deploy.ready("worker", timeout=0)

    def test_proxy_preserves_streams_upgrades_and_disables_retries(self):
        config = rolling.nginx_config("new-api-rolling-blue")
        for directive in ("proxy_buffering off", "proxy_request_buffering off", "proxy_next_upstream off", "proxy_set_header Upgrade"):
            self.assertIn(directive, config)
        self.assertNotIn("worker_shutdown_timeout", config)
        with self.assertRaises(rolling.DeployError):
            rolling.nginx_config("host; injected")

    def test_not_ready_response_honors_deadline(self):
        self.deploy.control = Mock(return_value={"contract": rolling.CONTRACT, "role": "worker", "ready": False,
                                               "instance": self.deploy.name("worker")})
        with self.assertRaises(rolling.DeployError):
            self.deploy.ready("worker", timeout=0)

    def test_proxy_dns_state_is_stable_and_isolated_between_slots(self):
        blue = rolling.nginx_config("new-api-rolling-blue")
        green = rolling.nginx_config("new-api-rolling-green")
        self.assertEqual(blue, rolling.nginx_config("new-api-rolling-blue"))
        self.assertNotEqual(re.search(r"zone (\w+) 64k;", blue)[1],
                            re.search(r"zone (\w+) 64k;", green)[1])
        self.assertIn("server new-api-rolling-blue:3000 resolve;", blue)
        self.assertIn("resolver 127.0.0.11 valid=1s ipv6=off;", blue)
        self.assertIn("resolver_timeout 2s;", blue)
        # No variable proxy_pass or URI suffix: preserve native paths and query strings.
        self.assertRegex(blue, r"proxy_pass http://web_[a-f0-9]+;")

    def drain_mocks(self, web, worker=None):
        """control() answers status from per-slot sequences; the last answer repeats."""
        answers = {"blue": list(web), "worker": list(worker or [{"active": 0, "pending": 0}])}
        calls = []

        def control(slot, path="ready", post=False):
            calls.append((slot, path))
            if path != "status":
                return {}
            sequence = answers[slot]
            return sequence.pop(0) if len(sequence) > 1 else sequence[0]
        self.deploy.control = Mock(side_effect=control)
        self.deploy.cut_gateway_connections = Mock(side_effect=lambda: calls.append(("gateway", "cut")))
        return calls

    def test_maintenance_requests_finishing_within_grace_are_not_cut(self):
        self.deploy.drain_grace = 10
        self.drain_mocks([{"active": 2, "pending": 1}, {"active": 0, "pending": 0}])
        started = rolling.time.monotonic()
        self.deploy.drain_maintenance("blue")
        self.assertLess(rolling.time.monotonic() - started, 5)
        self.deploy.cut_gateway_connections.assert_not_called()
        self.assertNotIn("maintenance_forced_cut", self.deploy.state)

    def test_maintenance_cuts_gateway_after_grace_then_waits_for_settlement(self):
        self.deploy.drain_grace = 0
        self.deploy.wait = 5
        calls = self.drain_mocks([{"active": 4, "pending": 6}, {"active": 0, "pending": 2}, {"active": 0, "pending": 0}],
                                 [{"active": 0, "pending": 1}, {"active": 0, "pending": 0}])
        with patch.object(rolling, "log") as log:
            self.deploy.drain_maintenance("blue")
        self.deploy.cut_gateway_connections.assert_called_once()
        # Statuses are polled again after the cut; no process is quiesced or stopped here.
        cut = calls.index(("gateway", "cut"))
        self.assertIn(("blue", "status"), calls[cut:])
        self.assertTrue(all(path == "status" for slot, path in calls if slot != "gateway"))
        messages = " ".join(call.args[0] for call in log.call_args_list)
        self.assertIn("MAINTENANCE FORCED CUT: active=4 pending=6", messages)
        self.assertIn("after 0s grace", messages)
        self.assertIn("settlement", messages)
        self.assertEqual(self.deploy.state["maintenance_forced_cut"]["active"], 4)

    def test_only_pending_settlement_after_grace_does_not_cut(self):
        self.deploy.drain_grace = 0
        self.deploy.wait = 5
        self.drain_mocks([{"active": 0, "pending": 3}, {"active": 0, "pending": 0}],
                         [{"active": 1, "pending": 2}, {"active": 0, "pending": 0}])
        self.deploy.drain_maintenance("blue")
        self.deploy.cut_gateway_connections.assert_not_called()

    def test_settlement_timeout_after_cut_cancels_before_migration(self):
        self.deploy.drain_grace = 0
        self.deploy.wait = 0
        self.drain_mocks([{"active": 3, "pending": 3}])
        self.deploy.reload = Mock()
        self.deploy.stop_quiesced = Mock()
        self.deploy.backup_maintenance = Mock()
        self.deploy.save(maintenance_source="blue")
        with patch.object(rolling.time, "sleep"):
            with self.assertRaisesRegex(rolling.DeployError, "settlement timed out.*web active=3 pending=3"):
                self.deploy.prepare_maintenance()
        self.deploy.cut_gateway_connections.assert_called_once()
        self.deploy.stop_quiesced.assert_not_called()
        self.deploy.backup_maintenance.assert_not_called()
        self.assertNotIn(("blue", "quiesce", True), [c.args for c in self.deploy.control.call_args_list])
        self.assertFalse(self.deploy.state.get("migration_started"))

    def test_maintenance_failure_resumes_old_service(self):
        self.deploy.save(previous=None)
        for name in ("cleanup", "ready", "verify_route", "check_backup_capacity", "continue_maintenance"):
            setattr(self.deploy, name, Mock())
        self.deploy.load_image = Mock(return_value="sha256:new")
        self.deploy.inspect = Mock(return_value={"Image": "sha256:old"})
        rolling.atomic_json(self.deploy.compose_path, {"services": {}})
        self.deploy.prepare_maintenance = Mock(side_effect=rolling.DeployError("maintenance settlement timed out"))
        self.deploy.resume_before_migration = Mock()
        with patch.object(rolling, "run"):
            with self.assertRaises(rolling.DeployError):
                self.deploy.maintenance(None, "sha256:" + "a" * 64)
        self.deploy.resume_before_migration.assert_called_once()
        self.deploy.continue_maintenance.assert_not_called()

    def test_gateway_cut_restarts_only_gateway_and_keeps_maintenance_gate(self):
        import http.server
        import threading
        flag = self.deploy.directory / "nginx" / "maintenance.flag"

        class Gateway(http.server.BaseHTTPRequestHandler):
            # Stand-in for the restarted nginx: the bind-mounted flag decides.
            def do_GET(handler):
                if flag.exists():
                    handler.send_response(503)
                    handler.send_header("X-Eflow-Maintenance", "1")
                else:
                    handler.send_response(200)
                handler.send_header("Content-Length", "0")
                handler.end_headers()

            def log_message(handler, *args):
                pass
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.deploy.gateway_origin = "http://127.0.0.1:%d" % server.server_address[1]
        commands = []
        with patch.object(rolling, "run", side_effect=lambda *args, **kw: commands.append((args, flag.exists())) or ""):
            with self.assertRaises(rolling.DeployError):
                self.deploy.cut_gateway_connections()  # never without the maintenance flag
            self.assertEqual(commands, [])
            self.deploy.set_maintenance(True)
            self.deploy.cut_gateway_connections()
        self.assertEqual(commands, [(("docker", "restart", "-t", "10", self.deploy.name("gateway")), True)])
        self.assertTrue(flag.exists())
        self.deploy.set_maintenance(False)
        with self.assertRaisesRegex(rolling.DeployError, "maintenance response"):
            self.deploy.verify_maintenance_gate(timeout=1)

    def test_maintenance_gate_survives_gateway_restart_in_config(self):
        # The flag is checked per request by every location that proxies to the
        # application, so a restarted nginx reading the same mount keeps it.
        config = rolling.nginx_config(self.deploy.name("blue"))
        self.assertEqual(config.count("if (-f /etc/nginx/maintenance.flag) { return 503; }"), 3)

    def test_blue_green_paths_never_cut_connections(self):
        self.deploy.cut_gateway_connections = Mock()
        self.deploy.save(pending_cleanup="green")
        self.deploy.running = Mock(return_value=True)
        self.deploy.gateway_retiring = Mock(return_value=False)
        self.deploy.control = Mock(return_value={"active": 5, "pending": 0})
        with patch.object(rolling, "run") as command:
            self.deploy.cleanup()
        self.assertEqual(self.deploy.state["pending_cleanup"], "green")
        self.assertFalse(any("restart" in c.args for c in command.call_args_list))
        self.handoff_mocks()
        self.deploy.cut_gateway_connections = Mock()
        self.deploy.control.return_value = {"active": 3, "pending": 0}
        with patch.object(rolling, "run") as command:
            with self.assertRaises(rolling.DeployError):
                self.deploy.handoff_worker("sha256:new")
        self.assertFalse(any("restart" in c.args for c in command.call_args_list))
        self.deploy.cut_gateway_connections.assert_not_called()

    def test_maintenance_recover_uses_persisted_boundary(self):
        self.deploy.save(phase="maintenance", maintenance_context="maintenance-" + "a" * 32, migration_started=False)
        self.deploy.resume_before_migration = Mock()
        self.deploy.continue_maintenance = Mock()
        self.deploy.recover()
        self.deploy.resume_before_migration.assert_called_once()
        self.deploy.continue_maintenance.assert_not_called()
        for phase in ("migrating", "starting", "maintenance_switching"):
            self.deploy.save(phase=phase, migration_started=True)
            self.deploy.recover()
        self.assertEqual(self.deploy.continue_maintenance.call_count, 3)
        self.deploy.resume_before_migration.assert_called_once()

    def test_old_resume_is_forbidden_after_migration_intent(self):
        self.deploy.save(migration_started=True)
        self.deploy.compose = Mock()
        with self.assertRaises(rolling.DeployError):
            self.deploy.resume_before_migration()
        self.deploy.compose.assert_not_called()

    def test_drain_failure_resumes_running_instances_without_recreation(self):
        context = self.deploy.directory / ("maintenance-" + "a" * 32)
        context.mkdir()
        original = {"state": {"phase": "stable", "active": "blue", "contract": "eflow-rolling-v6"},
                    "images": {"blue": "sha256:old", "worker": "sha256:old"},
                    "config": {"services": {"blue": {"image": "old"}, "worker": {"image": "old"}}}}
        rolling.atomic_json(context / "original.json", original)
        self.deploy.save(phase="maintenance", maintenance_source="blue", maintenance_context=context.name, migration_started=False)
        self.deploy.running = Mock(return_value=True)
        self.deploy.inspect = Mock(return_value={"Image": "sha256:old"})
        self.deploy.compose = Mock()
        self.deploy.control = Mock()
        self.deploy.ready = Mock()
        self.deploy.reload = Mock()
        self.deploy.verify_route = Mock()
        with patch.object(rolling, "run"):
            self.deploy.resume_before_migration()
        self.deploy.compose.assert_not_called()
        self.assertEqual(self.deploy.state["phase"], "stable")
        self.assertEqual(self.deploy.control.call_args_list[0].args, ("worker", "resume", True))
        self.assertEqual(self.deploy.control.call_args_list[1].args, ("blue", "resume", True))

    def test_completed_migration_is_observed_after_client_disconnect(self):
        context = self.deploy.directory / ("maintenance-" + "b" * 32)
        context.mkdir()
        self.deploy.save(phase="migrating", maintenance_context=context.name, maintenance_image="sha256:new",
                         migration_container="retained-job", migration_started=True)
        rolling.atomic_json(self.deploy.compose_path, {"services": {"migrate": {"environment": {"SQL_DSN": "u:private@tcp(db:3306)/db"}}}})
        self.deploy.migration_exists = Mock(return_value=True)
        self.deploy.inspect = Mock(return_value={"Image": "sha256:new", "State": {"Status": "exited", "Running": False, "ExitCode": 0}})
        self.deploy.compose = Mock()
        logs = type("Result", (), {"returncode": 0, "stdout": b"completed", "stderr": b""})()
        with patch.object(rolling.subprocess, "run", return_value=logs):
            self.deploy.run_maintenance_migration()
        self.deploy.compose.assert_not_called()
        self.assertEqual(self.deploy.state["phase"], "starting")

    def test_failed_migration_keeps_stderr_and_redacts_credentials(self):
        context = self.deploy.directory / ("maintenance-" + "c" * 32)
        context.mkdir()
        self.deploy.save(phase="migrating", maintenance_context=context.name, maintenance_image="sha256:new",
                         migration_container="failed-job", migration_started=True)
        rolling.atomic_json(self.deploy.compose_path, {"services": {"migrate": {"environment": {"SQL_DSN": "u:private@tcp(db:3306)/db"}}}})
        self.deploy.migration_exists = Mock(return_value=True)
        self.deploy.inspect = Mock(return_value={"Image": "sha256:new", "State": {"Status": "exited", "Running": False, "ExitCode": 1}})
        logs = type("Result", (), {"returncode": 0, "stdout": b"", "stderr": b"fatal password=private\nu:private@tcp(db:3306)/db"})()
        with patch.object(rolling.subprocess, "run", return_value=logs):
            with self.assertRaises(rolling.DeployError):
                self.deploy.run_maintenance_migration()
        evidence = json.loads((context / "failed-job.json").read_text())
        self.assertEqual(evidence["exit_code"], 1)
        self.assertIn("fatal", evidence["log"])
        self.assertNotIn("private", evidence["log"])
        self.assertTrue(self.deploy.state["migration_failed"])

    def test_starting_recovery_does_not_rerun_migration(self):
        self.deploy.save(phase="starting", maintenance_context="maintenance-" + "a" * 32,
                         maintenance_image="sha256:new", maintenance_source="blue", migration_started=True)
        rolling.atomic_json(self.deploy.compose_path, {"services": {s: {"image": "old"} for s in ("blue", "green", "worker", "migrate")}})
        self.deploy.verify_maintenance_backup = Mock()
        self.deploy.run_maintenance_migration = Mock()
        self.deploy.compose = Mock()
        self.deploy.ready = Mock()
        self.deploy.reload = Mock()
        self.deploy.verify_route = Mock()
        with patch.object(rolling, "run", return_value='{"success":true}'):
            self.deploy.recover()
        self.deploy.run_maintenance_migration.assert_not_called()
        self.assertEqual(self.deploy.state["phase"], "stable")
        self.assertEqual(self.deploy.state["worker_image"], "sha256:new")


class AuthenticationConfigTests(unittest.TestCase):
    def test_existing_cookie_keys_survive_switch(self):
        env = {"SESSION_SECRET": "session-test", "EFLOW_AUTH_SECRET": "identity-test"}
        rolling.verify_auth_environment(env, dict(env))

    def test_missing_or_changed_keys_stop_switch_without_secret_disclosure(self):
        before = {"SESSION_SECRET": "private-session", "EFLOW_AUTH_SECRET": "private-identity"}
        for key in ("SESSION_SECRET", "EFLOW_AUTH_SECRET", "SESSION_COOKIE_SECURE"):
            after = dict(before, **{key: "different-private-value"})
            with self.subTest(key=key), self.assertRaises(rolling.DeployError) as raised:
                rolling.verify_auth_environment(before, after)
            self.assertNotIn("private", str(raised.exception))
        with self.assertRaises(rolling.DeployError):
            rolling.verify_auth_environment({}, {})

    def test_mismatch_prevents_gateway_reload(self):
        with tempfile.TemporaryDirectory() as root:
            deploy = rolling.Deployment(root)
            deploy.state = {"active": "blue"}
            deploy.inspect = Mock(side_effect=[{"Config": {"Env": ["SESSION_SECRET=a"]}}, {"Config": {"Env": ["SESSION_SECRET=b"]}}])
            deploy.reload = Mock()
            with self.assertRaises(rolling.DeployError):
                deploy.switch("green")
            deploy.reload.assert_not_called()


if __name__ == "__main__":
    unittest.main()
