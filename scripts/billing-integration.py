"""Compare five actual relay settlements across old/new images using local usage."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parent
from pricing_equivalence import compare
assert os.environ.get('DRILL_CANDIDATE_IMAGE'), 'Candidate is required'
baseline = Path(sys.argv[1]).resolve()
original_argv = sys.argv[:]
sys.argv = [str(ROOT/'maintenance-integration.py'), '--fixtures', str(baseline.parent), '--evidence-dir', os.environ['DRILL_EVIDENCE_DIR'], '--old-web', os.environ['DRILL_OLD_WEB'], '--old-worker', os.environ['DRILL_OLD_WORKER'], '--old-contract', os.environ['DRILL_OLD_CONTRACT']]
spec = importlib.util.spec_from_file_location('matrix_fixture', ROOT/'maintenance-integration.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
sys.argv = original_argv
m.ORIGIN = 'http://127.0.0.1:31892'
m.infra['services']['upstream']['ports'] = ['127.0.0.1:31893:8080']
m.baseline_config['services']['gateway']['ports'] = ['127.0.0.1:31892:3000']
baseline = Path(sys.argv[1]).resolve()
assert baseline.name == 'baseline.sql' and baseline.parent.name.startswith('pricing-drill-')
report = {'scope':'actual local relay billing comparison, simulated upstream; not Jev protocol validation',
          'baseline_sha256':hashlib.sha256(baseline.read_bytes()).hexdigest(), 'samples':[]}
holder = m.PREFIX + '-ip-holder'
holder_created = False
m.infra['services']['upstream']['command'] = ['node', '/src/scripts/billing-test-upstream.cjs']
cases = [
    ('legacy_multimodal_audio', 'qwen3.5-omni-flash', '/v1/chat/completions'),
    ('default_output_ratio_and_cache', 'gpt-5-codex', '/v1/chat/completions'),
    ('flat_expression_and_cache', 'qwen3.8-omni-flash', '/v1/chat/completions'),
    ('jev_expression_zero_output', 'jev-1.13', '/v1/chat/completions'),
    ('knowledge_embedding', 'qwen3.7-text-embedding', '/v1/embeddings'),
]

def save():
    m.rolling.atomic_json(m.ROOT/'billing-matrix.json', report)

def settle_all(token):
    result=[]
    for name, model, endpoint in cases:
        before = int(m.sql('SELECT used_quota FROM users WHERE id=2'))
        token_before = int(m.sql('SELECT used_quota FROM tokens WHERE user_id=2 ORDER BY id DESC LIMIT 1'))
        last = int(m.sql('SELECT COALESCE(MAX(id),0) FROM logs WHERE user_id=2 AND type=2'))
        body = {'model':model,'input':'local-only'} if endpoint.endswith('embeddings') else {
            'model':model,'messages':[{'role':'user','content':'local-only'}],'stream':False}
        req=urllib.request.Request(m.ORIGIN+endpoint,data=json.dumps(body).encode(),
            headers={'Content-Type':'application/json','Authorization':'Bearer sk-'+token})
        with m.opener.open(req,timeout=20) as response:
            assert response.status==200
            response_body=json.load(response)
            assert 'data' in response_body if endpoint.endswith('embeddings') else 'choices' in response_body
        for _ in range(40):
            rows=m.sql("SELECT JSON_OBJECT('id',id,'model',model_name,'input',prompt_tokens,'output',completion_tokens,'quota',quota) FROM logs WHERE user_id=2 AND type=2 AND id>"+str(last)).splitlines()
            if rows: break
            time.sleep(0.25)
        assert len(rows)==1, 'Expected one settled consumption log'
        row=json.loads(rows[0]);row['case']=name
        assert row['model']==model and row['quota']>0
        after=int(m.sql('SELECT used_quota FROM users WHERE id=2'))
        token_after=int(m.sql('SELECT used_quota FROM tokens WHERE user_id=2 ORDER BY id DESC LIMIT 1'))
        assert after-before==token_after-token_before==row['quota'],'Wallet/token/log mismatch'
        row['wallet_and_token_match']=True
        result.append(row)
    return result

try:
    for port in [31892,31893]:
        with socket.socket() as probe: probe.bind(('127.0.0.1',port))
    m.rolling.atomic_json(m.infra_path,m.infra)
    m.rolling.atomic_json(m.deployment.compose_path,m.baseline_config)
    m.deployment.state=m.baseline_state.copy();m.deployment.save();m.deployment.write_route('blue')
    print('BILLING_MATRIX_ROOT '+str(m.ROOT),flush=True)
    m.run('docker','compose','-f',str(m.infra_path),'up','-d',timeout=180)
    deadline=time.monotonic()+90
    while True:
        try:
            m.sql('SELECT 1');break
        except RuntimeError:
            if time.monotonic()>deadline:raise
            time.sleep(1)
    m.run('docker','exec','-i','-e','MYSQL_PWD='+m.PASSWORD,m.MYSQL,'mysql','-uroot','drill',input_data=baseline.read_bytes())
    # Baselines that already have scoped access tokens get a temporary nap_ row
    # (legacy tokens may be retired there); older baselines use the legacy token,
    # which the upgraded version keeps accepting during its transition period.
    if m.sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='user_access_tokens'")!='0':
        admin='nap_'+os.urandom(22).hex()[:43]
        scopes=json.dumps(json.loads((Path(__file__).with_name('release-pat-scopes.json')).read_text(encoding='utf-8')))
        now=int(time.time())
        m.sql("INSERT INTO user_access_tokens (user_id,name,token_hash,token_hint,scopes,expires_at,last_used_at,last_used_ip,created_at) VALUES (2,'__billing_integration','"
              +hashlib.sha256(admin.encode()).hexdigest()+"','"+admin[-4:]+"','"+scopes.replace("'","''")+"',"+str(now+7200)+",0,'',"+str(now)+")")
    else:
        admin=os.urandom(16).hex()
        m.sql("UPDATE users SET access_token='"+admin+"' WHERE id=2")
    m.opener.addheaders=[('Authorization','Bearer '+admin)]
    m.deployment.compose('up','-d','--no-deps','worker','blue')
    m.deployment.ready('worker',expected_contract=os.environ['DRILL_OLD_CONTRACT']);m.deployment.ready('blue',expected_contract=os.environ['DRILL_OLD_CONTRACT'])
    m.deployment.compose('up','-d','--no-deps','gateway');m.wait_api()
    cid=int(m.sql("SELECT id FROM channels WHERE name='isolated-mock'"))
    m.api('/api/channel/',{'id':cid,'models':','.join(x[1] for x in cases),'priority':999999},'PUT')
    token=m.sql('SELECT `key` FROM tokens WHERE user_id=2 ORDER BY id DESC LIMIT 1')
    prices=m.price_snapshot()
    old=settle_all(token)
    print('OLD_BILLING '+json.dumps(old),flush=True)
    report['old']=old;save()
    tag='new-api-eflow:release-'+m.PREFIX
    archive=m.ROOT/'candidate.tar'
    m.run('docker','tag',m.NEW,tag);m.run('docker','save','-o',str(archive),tag,timeout=180)
    m.deployment.maintenance(archive,tag);m.wait_api()
    new=settle_all(token)
    for a,b in zip(old,new):
        report['samples'].append({'case':a['case'],'model':a['model'],'input':a['input'],'output':a['output'],
             'old_quota':a['quota'],'new_quota':b['quota'],'delta':b['quota']-a['quota'],
             'wallet_token_logs_match':True, 'equal':all(a[k]==b[k] for k in ['case','model','input','output','quota'])})
    save()
    assert all(row['equal'] for row in report['samples']), 'Old/new settlement differs; see billing-matrix.json'
    migrated=m.price_snapshot()
    assert migrated['GroupRatio']==prices['GroupRatio'] and migrated['GroupGroupRatio']==prices['GroupGroupRatio']
    report['all_model_pricing'] = compare(prices, migrated)
    save()
    report['restart_checks']=[]
    def addresses():
        return {slot:{name:network['IPAddress'] for name,network in m.deployment.inspect(m.deployment.name(slot))['NetworkSettings']['Networks'].items()}
                for slot in ['worker','blue']}
    for cycle in range(3):
        before_ips=addresses()
        m.deployment.compose('restart','worker','blue')
        m.deployment.ready('worker');m.deployment.ready('blue')
        after_ips=addresses()
        status=m.endpoint_state()
        observation={'cycle':cycle+1,'before_ips':before_ips,'after_ips':after_ips,'http_before_gateway_reload':status}
        if status!=200:
            logs=m.run('docker','logs','--tail','25',m.deployment.name('gateway')).decode('utf-8')
            # docker logs emits nginx errors on stderr; preserve them privately without credentials.
            import subprocess
            log_result=subprocess.run(['docker','logs','--tail','25',m.deployment.name('gateway')],capture_output=True)
            (m.ROOT/('restart-gateway-'+str(cycle+1)+'.log')).write_bytes(log_result.stdout+log_result.stderr)
        m.deployment.verify_route('blue');m.wait_api()
        assert m.price_snapshot()==migrated,'Pricing changed after application restart'
        observation.update(http_after_automatic_recovery=200,manual_reload=False,pricing_unchanged=True)
        report['restart_checks'].append(observation);save()
        print('RESTART_CHECK '+json.dumps(observation),flush=True)
    report['restart_idempotent']=True
    blue_name=m.deployment.name('blue')
    old_ip=addresses()['blue'][m.network]
    m.run('docker','network','disconnect',m.network,blue_name)
    m.run('docker','run','-d','--name',holder,'--network',m.network,'--ip',old_ip,'alpine:3.22','sleep','180')
    holder_created=True
    m.run('docker','network','connect','--alias',blue_name,m.network,blue_name)
    new_ip=addresses()['blue'][m.network]
    assert new_ip!=old_ip
    m.deployment.ready('blue')
    initial_status=m.endpoint_state()
    started=time.monotonic()
    m.deployment.verify_route('blue');m.wait_api()
    assert m.price_snapshot()==migrated
    report['address_change_check']={'old_ip':old_ip,'new_ip':new_ip,'initial_http':initial_status,
        'after_automatic_recovery':200,'manual_reload':False,'recovery_seconds':round(time.monotonic()-started,2),'pricing_unchanged':True}
    print('ADDRESS_CHANGE_CHECK '+json.dumps(report['address_change_check']),flush=True)
    report.update(result='passed',old_image_id=m.image_id(m.OLD_WEB),candidate_image_id=m.image_id(m.NEW),
                  deployment_script_sha256=hashlib.sha256((m.REPO/'scripts/rolling-deploy.py').read_bytes()).hexdigest(),
                  protected_pricing_models=len(migrated['ModelPricing']['models']),supplier_requests=0)
    save();print('BILLING_MATRIX_AND_RESTART_PASSED '+json.dumps(report['samples']),flush=True)
finally:
    if holder_created:
        assert holder.startswith('pricing-drill-')
        m.run('docker','rm','-f',holder)
    assert m.PREFIX.startswith('pricing-drill-') and m.deployment.root==m.ROOT.resolve()
    m.deployment.compose('stop','gateway','blue','green','worker',timeout=160)
    m.retain_audit('billing-final')
    m.deployment.compose('down','--volumes','--remove-orphans',timeout=120)
    m.run('docker','compose','-f',str(m.infra_path),'down','--volumes',timeout=120)
    report['isolated_resources_cleaned']=True;save()
