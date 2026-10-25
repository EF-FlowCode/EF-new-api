"""Opt-in, isolated Docker test of gateway DNS, slot switching and wire behavior.

Uses existing nginx:1.28-alpine, node:20-bookworm and alpine:3.22 images; no build,
database, credentials or supplier calls. Only UUID-named test resources are removed.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import socket
import tempfile
import time
import urllib.error
import urllib.request
import uuid

spec = importlib.util.spec_from_file_location("rolling", Path(__file__).with_name("rolling-deploy.py"))
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)

MOCK = r"""
const http = require('node:http');
const slot = process.env.SLOT;
let attempts = 0;
const streams = new Set();
const server = http.createServer((req, res) => {
  res.setHeader('X-New-Api-Instance', slot);
  if (req.url === '/stream') {
    res.writeHead(200, {'Content-Type': 'text/event-stream'});
    res.write('data: ' + slot + '\n\n');
    streams.add(res); res.on('close', () => streams.delete(res)); return;
  }
  if (req.url === '/release') {
    for (const stream of streams) stream.end('data: finished ' + slot + '\n\n');
    res.end('released'); return;
  }
  if (req.url === '/fail') { attempts++; req.resume(); req.socket.destroy(); return; }
  if (req.url === '/attempts') { res.end(JSON.stringify({attempts})); return; }
  let body = '';
  req.on('data', chunk => body += chunk);
  req.on('end', () => res.end(JSON.stringify({url: req.url, body, headers: req.headers, slot})));
});
server.on('upgrade', (req, socket) => {
  socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n');
  socket.on('data', chunk => socket.write(chunk));
});
server.listen(3000, '0.0.0.0');
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-dir', type=Path, default=Path(tempfile.gettempdir()))
    args = parser.parse_args()
    args.evidence_dir.mkdir(parents=True, exist_ok=True)
    prefix = 'gateway-dns-' + uuid.uuid4().hex[:8]
    root = Path(tempfile.mkdtemp(prefix=prefix + '-', dir=args.evidence_dir))
    rolling.PREFIX = prefix
    deployment = rolling.Deployment(root)
    network = prefix + '-network'
    report = {'root': str(root), 'checks': [], 'result': 'running'}
    services = {}
    for slot in ['blue', 'green']:
        services[slot] = {'image': 'node:20-bookworm', 'container_name': deployment.name(slot),
            'command': ['node', '-e', MOCK], 'environment': {'SLOT': deployment.name(slot)}, 'networks': ['runtime']}
    services['gateway'] = {'image': rolling.GATEWAY_IMAGE, 'container_name': deployment.name('gateway'),
        'networks': ['runtime', 'front'], 'ports': ['127.0.0.1::3000'],
        'volumes': [{'type': 'bind', 'source': str(deployment.directory / 'nginx'),
                     'target': '/etc/nginx', 'read_only': True}]}
    from release_baseline import validation_networks
    rolling.atomic_json(deployment.compose_path, {'services': services,
        'networks': validation_networks({'runtime': {'name': network, 'internal': True}, 'front': {}})})
    deployment.write_route('blue')
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    stream = upgraded = None
    holders = []
    try:
        # A temporarily absent DNS name must not prevent gateway startup.
        deployment.compose('up', '-d', 'gateway')
        rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-t')
        port = int(deployment.inspect(deployment.name('gateway'))['NetworkSettings']['Ports']['3000/tcp'][0]['HostPort'])
        origin = 'http://127.0.0.1:' + str(port)

        def request(path, data=None):
            req = urllib.request.Request(origin + path, data=data,
                headers={'Authorization': 'Bearer synthetic-gateway-test', 'Content-Type': 'application/json'})
            return client.open(req, timeout=4)

        def wait_slot(slot, timeout=15):
            started = time.monotonic()
            while time.monotonic() - started < timeout:
                try:
                    with request('/api/status') as response:
                        if response.headers.get('X-New-Api-Instance') == deployment.name(slot):
                            return round(time.monotonic() - started, 2)
                except OSError:
                    pass
                time.sleep(0.2)
            raise AssertionError('gateway did not recover to ' + slot)

        def force_address_change():
            name = deployment.name('blue')
            old = deployment.inspect(name)['NetworkSettings']['Networks'][network]['IPAddress']
            rolling.run('docker', 'network', 'disconnect', network, name)
            holder = prefix + '-holder-' + str(len(holders))
            holders.append(holder)
            rolling.run('docker', 'run', '-d', '--name', holder, '--network', network,
                        '--ip', old, 'alpine:3.22', 'sleep', '300')
            rolling.run('docker', 'network', 'connect', '--alias', name, network, name)
            new = deployment.inspect(name)['NetworkSettings']['Networks'][network]['IPAddress']
            assert new != old
            return {'before': old, 'after': new}

        deployment.compose('up', '-d', 'blue', 'green')
        wait_slot('blue')
        report['checks'].append({'name': 'missing_upstream_at_startup', 'result': 'passed'})
        path = '/v1/systemone?path=a%2Fb&plus=a+b&repeat=1&repeat=2'
        payload = b'{"state":["native"],"questions":{}}'
        with request(path, payload) as response:
            echo = json.load(response)
        assert echo['url'] == path and echo['body'].encode() == payload
        assert echo['headers']['authorization'] == 'Bearer synthetic-gateway-test'
        assert echo['headers']['host'] == '127.0.0.1:' + str(port)
        report['checks'].append({'name': 'path_query_body_authorization', 'result': 'passed'})

        stream = request('/stream')
        assert stream.readline() == ('data: ' + deployment.name('blue') + '\n').encode()
        upgraded = socket.create_connection(('127.0.0.1', port), timeout=4)
        upgraded.sendall(b'GET /socket HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n')
        headers = b''
        while b'\r\n\r\n' not in headers:
            chunk = upgraded.recv(1024)
            assert chunk, 'upgrade connection closed before handshake'
            headers += chunk
        assert b'101 Switching Protocols' in headers
        deployment.reload('green'); wait_slot('green')
        frame = b'\x81\x82\x01\x02\x03\x04ik'  # masked "hi"; check bidirectional byte transport
        upgraded.sendall(frame)
        received = b''
        while len(received) < len(frame):
            chunk = upgraded.recv(len(frame) - len(received))
            assert chunk, 'upgrade connection closed during slot switch'
            received += chunk
        assert received == frame
        rolling.run('docker', 'exec', deployment.name('blue'), 'node', '-e',
                    "fetch('http://127.0.0.1:3000/release').then(r=>r.text()).then(console.log)")
        assert ('data: finished ' + deployment.name('blue')).encode() in stream.read()
        stream.close(); stream = None
        upgraded.close(); upgraded = None
        report['checks'].append({'name': 'sse_and_upgrade_survive_slot_switch', 'result': 'passed'})
        try:
            with request('/fail', b'{"paid":true}'):
                raise AssertionError('expected upstream failure')
        except urllib.error.HTTPError as error:
            assert error.code == 502
        with request('/attempts') as response:
            assert json.load(response)['attempts'] == 1
        report['checks'].append({'name': 'failed_post_forwarded_once', 'result': 'passed'})

        # Reproduce the old static behavior against the same mock and Docker network.
        config = rolling.nginx_config(deployment.name('blue'))
        static = re.sub(r'    upstream web_[a-f0-9]+ \{.*?\n    \}\n', '', config, flags=re.S)
        static = re.sub(r'proxy_pass http://web_[a-f0-9]+;',
                        'proxy_pass http://' + deployment.name('blue') + ':3000;', static)
        config_path = deployment.directory / 'nginx' / 'nginx.conf'
        config_path.write_text(static, encoding='utf-8')
        rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-t')
        rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-s', 'reload')
        wait_slot('blue')
        observation = force_address_change()
        for _ in range(3):
            time.sleep(1)
            try:
                with request('/api/status'):
                    raise AssertionError('old static config unexpectedly recovered')
            except urllib.error.HTTPError as error:
                assert error.code == 502
        report['checks'].append(dict(name='old_static_dns_reproduces_502', result='passed', **observation))
        deployment.reload('blue'); wait_slot('blue')
        master_before = deployment.inspect(deployment.name('gateway'))['State']['StartedAt']
        for cycle in range(2):
            observation = force_address_change()
            seconds = wait_slot('blue')
            report['checks'].append(dict(name='dynamic_dns_address_change', cycle=cycle + 1,
                result='passed', recovery_seconds=seconds, manual_reload=False, **observation))
        assert deployment.inspect(deployment.name('gateway'))['State']['StartedAt'] == master_before
        # Admission gate is a file, not tied to the business process or a new
        # nginx worker. Existing SSE continues, new native requests get JSON.
        stream = request('/stream')
        assert stream.readline().startswith(b'data: ')
        deployment.set_maintenance(True)
        for path, accept, data, expected in [
            ('/pricing', 'text/html', None, 'text/html'),
            ('/v1/systemone', 'text/html', b'{"model":"test"}', 'application/json'),
            ('/api/status', 'text/html', None, 'application/json'),
            ('/v1/messages', 'application/json', b'{}', 'application/json')]:
            req = urllib.request.Request(origin + path, data=data, headers={'Accept': accept})
            try:
                client.open(req, timeout=4)
                raise AssertionError('maintenance request was admitted')
            except urllib.error.HTTPError as response:
                body = response.read()
                assert response.code == 503
                assert response.headers['Retry-After'] == '30'
                assert response.headers['Cache-Control'] == 'no-store'
                assert expected in response.headers['Content-Type']
                if expected == 'application/json':
                    assert json.loads(body)['error']['code'] == 'maintenance'
                else:
                    assert '正在维护升级'.encode() in body
        rolling.run('docker', 'exec', deployment.name('blue'), 'node', '-e',
                    "fetch('http://127.0.0.1:3000/release').then(r=>r.text())")
        assert b'finished' in stream.read()
        stream.close(); stream = None
        deployment.compose('stop', 'blue', 'green')
        with client.open(origin + '/__maintenance/status') as response:
            assert json.load(response)['maintenance'] is True
        try:
            client.open(urllib.request.Request(origin + '/', headers={'Accept': 'text/html'}))
        except urllib.error.HTTPError as response:
            assert response.code == 503 and '正在维护升级'.encode() in response.read()
        else:
            raise AssertionError('stopped business app must still show the maintenance page')
        with client.open(origin + '/__maintenance/page.js') as response:
            assert b'checkMaintenance' in response.read()
        deployment.compose('start', 'blue', 'green')
        deployment.set_maintenance(False)
        wait_slot('blue')
        with client.open(origin + '/__maintenance/status') as response:
            assert json.load(response)['maintenance'] is False
        assert deployment.inspect(deployment.name('gateway'))['State']['StartedAt'] == master_before
        report['checks'].append({'name': 'maintenance_page_api_sse_stopped_app_recovery', 'result': 'passed'})
        report['result'] = 'passed'
    finally:
        if stream: stream.close()
        if upgraded: upgraded.close()
        assert prefix.startswith('gateway-dns-') and deployment.root == root.resolve()
        for holder in holders:
            rolling.run('docker', 'rm', '-f', holder)
        deployment.compose('down', '--volumes', '--remove-orphans')
        report['isolated_resources_cleaned'] = True
        rolling.atomic_json(root / 'report.json', report)
        print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
