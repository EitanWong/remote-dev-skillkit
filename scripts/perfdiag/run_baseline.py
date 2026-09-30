#!/usr/bin/env python3
"""Opt-in isolated synthetic perf diagnostic; never contacts a gateway/host.

Run from a clean baseline worktree with the perfdiag tagged tests present.
Raw profiles may contain only these synthetic fixture allocations. No production
snapshots, credentials, command-lines or process environments are collected.
This small-sample run is NOT full-scale QA or a soak/release PASS.
"""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--out', required=True, type=Path)
p.add_argument('--scratch', type=Path, default=Path(os.environ.get('TMPDIR', '')))
p.add_argument('--events', default='1000,10000,20000')
p.add_argument('--clients', default='20,40')
p.add_argument('--polls', type=int, default=2)
p.add_argument('--large', action='store_true', help='explicit large-history opt-in; coordinate resources first')
p.add_argument('--skip-profiles', action='store_true')
a = p.parse_args()
if not str(a.scratch) or not a.scratch.is_absolute():
    p.error('--scratch or TMPDIR must be an absolute profile scratch path')
counts = [int(x) for x in a.events.split(',')]
clients = [int(x) for x in a.clients.split(',')]
if not counts or min(counts) < 1 or max(counts) > 700000 or len(set(counts)) != len(counts):
    p.error('events must be distinct integers 1..700000')
if max(counts) > 20000 and not a.large:
    p.error('histories >20000 need --large and resource coordination')
if not clients or min(clients) < 1 or max(clients) > 80 or not 1 <= a.polls <= 20:
    p.error('clients bound 1..80; polls bound 1..20')
OUT = a.out.resolve()
OUT.mkdir(parents=True, exist_ok=True)
a.scratch.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(TMPDIR=str(a.scratch), GOCACHE=str(a.scratch / 'go-build'),
           GOMAXPROCS='2', GOMEMLIMIT='512MiB', GOPROXY='off', GOSUMDB='off',
           PERF_DIAG_EVENTS=a.events, PERF_DIAG_CLIENTS=a.clients,
           PERF_DIAG_POLLS=str(a.polls))
env.pop('PERF_DIAG_ENFORCE_SLO', None)
if a.large:
    env['PERF_DIAG_LARGE'] = '1'
else:
    env.pop('PERF_DIAG_LARGE', None)
receipts, samples, benches, summaries, requests, persists, io_summaries = [], [], [], [], [], [], []
CG = Path('/sys/fs/cgroup') / Path('/proc/self/cgroup').read_text().split('::', 1)[1].strip().lstrip('/')

def numeric_text(path):
    try:
        return Path(path).read_text().strip()
    except (FileNotFoundError, PermissionError, ProcessLookupError):
        return None


def available():
    return int(dict(x.split(':', 1) for x in Path('/proc/meminfo').read_text().splitlines())['MemAvailable'].split()[0]) * 1024


def cgroup():
    return {n: numeric_text(CG / n) for n in ['memory.max', 'memory.current', 'memory.events', 'memory.swap.max', 'cpu.stat']}


def sample(label, proc, started):
    row = {'command': label, 'pid': proc.pid, 'elapsed_s': time.monotonic() - started}
    try:
        status = dict(x.split(':', 1) for x in Path(f'/proc/{proc.pid}/status').read_text().splitlines() if ':' in x)
        for n in ['VmRSS', 'VmHWM', 'VmSwap']:
            row[n + '_bytes'] = int(status.get(n, '0 kB').split()[0]) * 1024
        row['threads'] = int(status.get('Threads', '0').strip())
        row['fd_count'] = len(list(Path(f'/proc/{proc.pid}/fd').iterdir()))
        smaps = numeric_text(f'/proc/{proc.pid}/smaps_rollup')
        if smaps:
            d = dict(x.split(':', 1) for x in smaps.splitlines() if ':' in x)
            row['pss_bytes'] = int(d.get('Pss', '0 kB').split()[0]) * 1024
        io = numeric_text(f'/proc/{proc.pid}/io')
        if io:
            for n, v in (x.split(':', 1) for x in io.splitlines()):
                row[n] = int(v.strip())
        stat = Path(f'/proc/{proc.pid}/stat').read_text().rsplit(')', 1)[1].split()
        row['cpu_user_s'] = int(stat[11]) / os.sysconf('SC_CLK_TCK')
        row['cpu_system_s'] = int(stat[12]) / os.sysconf('SC_CLK_TCK')
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        return None
    return row


def save():
    (OUT / 'commands.json').write_text(json.dumps(receipts, indent=2) + '\n')
    (OUT / 'PROCESS_SAMPLES.json').write_text(json.dumps(samples, indent=2) + '\n')
    for name, rows in [('BENCH_RESULTS', benches), ('HTTP_SUMMARIES', summaries), ('HTTP_REQUESTS', requests), ('PROCESS_SAMPLES', samples), ('PERSIST_SAMPLES', persists), ('IO_SUMMARIES', io_summaries)]:
        (OUT / (name + '.json')).write_text(json.dumps(rows, indent=2) + '\n')
        if rows:
            columns = sorted({k for r in rows for k in r})
            with (OUT / (name + '.csv')).open('w', newline='') as f:
                w = csv.DictWriter(f, fieldnames=columns, lineterminator='\n')
                w.writeheader()
                w.writerows(rows)


def run(label, cmd, expected=(0,), extra=None, timeout=120):
    free = available()
    if free < 512 * 1024**2:
        raise RuntimeError('shared host MemAvailable <512MiB; STOP, not PASS')
    e = env.copy()
    e.update(extra or {})
    started = time.monotonic()
    receipt = {'label': label, 'argv': [str(x) for x in cmd], 'command': shlex.join([str(x) for x in cmd]),
               'cwd': str(ROOT), 'synthetic': True, 'start_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
               'environment': {k: e.get(k) for k in ['TMPDIR', 'GOCACHE', 'GOMAXPROCS', 'GOMEMLIMIT', 'GOPROXY', 'GOSUMDB', 'PERF_DIAG_EVENTS', 'PERF_DIAG_CLIENTS', 'PERF_DIAG_POLLS', 'PERF_DIAG_LARGE', 'PERF_DIAG_ENFORCE_SLO']},
               'mem_available_before_bytes': free, 'cgroup_before': cgroup(), 'log': label + '.log', 'expected_exit_codes': list(expected)}
    with (OUT / (label + '.log')).open('w') as f:
        proc = subprocess.Popen([str(x) for x in cmd], cwd=ROOT, env=e, stdout=f, stderr=subprocess.STDOUT)
        abort = None
        while proc.poll() is None:
            row = sample(label, proc, started)
            if row:
                samples.append(row)
                if row.get('VmRSS_bytes', 0) > 1024**3:
                    abort = 'harness RSS safety bound >1GiB; terminated, NOT a Go/cgroup OOM attribution'
            if time.monotonic() - started > timeout:
                abort = 'harness command timeout; terminated'
            if abort:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
                break
            time.sleep(0.1)
        rc = proc.wait()
    receipt.update(exit_code=rc, elapsed_s=time.monotonic() - started, cgroup_after=cgroup(), abort_reason=abort)
    receipts.append(receipt)
    text = (OUT / (label + '.log')).read_text()
    for line in text.splitlines():
        m = re.match(r'^(Benchmark\S+)\s+(\d+)\s+([\d.eE+-]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op', line)
        if m:
            benches.append({'run': label, 'benchmark': m[1], 'iterations': int(m[2]), 'ns_per_op': float(m[3]), 'bytes_per_op': int(m[4]), 'allocs_per_op': int(m[5])})
        for marker, rows in [('PERF_SUMMARY ', summaries), ('PERF_REQUEST ', requests), ('PERF_PERSIST ', persists), ('PERF_IO_SUMMARY ', io_summaries)]:
            if marker in line:
                payload = json.loads(line.split(marker, 1)[1])
                payload['run'] = label
                rows.append(payload)
    save()
    print(json.dumps({'label': label, 'exit_code': rc, 'elapsed_s': receipt['elapsed_s'], 'abort_reason': abort}), flush=True)
    if abort or rc not in expected:
        raise RuntimeError(f'{label} unexpected exit {rc}; see {receipt["log"]}')
    if '-test.v' in cmd and '=== RUN' not in text:
        raise RuntimeError(f'{label}: no test matched; UNKNOWN not PASS')
    if rc == 1 and 1 in expected and not ('exceeds' in text or 'exceeded' in text):
        raise RuntimeError('expected RED must show the exact growth symptom, not an unrelated failure')
    return text


metadata = {'baseline_sha': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
            'go': subprocess.check_output(['go', 'version'], cwd=ROOT, text=True).strip(),
            'synthetic': True, 'seed': 20260930, 'history_counts': counts, 'client_counts': clients,
            'polls_per_client': a.polls, 'scope': 'gateway microbench and in-process HTTP handler; no TCP/MCP/host/production',
            'cgroup_path': str(CG), 'profile_sample_period_s': 0.1,
            'harness_sha256': {str(x.relative_to(ROOT)): hashlib.sha256(x.read_bytes()).hexdigest() for x in [Path(__file__), ROOT/'internal/gateway/perfdiag_test.go', ROOT/'internal/gateway/perfdiag_events_test.go', ROOT/'internal/httpapi/perfdiag_test.go', ROOT/'internal/httpapi/perfdiag_io_test.go']}}
(OUT / 'RUN_METADATA.json').write_text(json.dumps(metadata, indent=2) + '\n')
try:
    gwbin, httpbin = OUT / 'gateway.test', OUT / 'httpapi.test'
    run('build-gateway', ['go', 'test', '-p=1', '-tags=perfdiag', '-c', '-o', gwbin, './internal/gateway'], timeout=180)
    run('build-httpapi', ['go', 'test', '-p=1', '-tags=perfdiag', '-c', '-o', httpbin, './internal/httpapi'], timeout=180)
    run('fixture', [gwbin, '-test.run=^TestPerfHistoryFixtureIntegrity$', '-test.count=1', '-test.v', '-test.timeout=60s'])
    run('SnapshotGrowthDiagnostic', [gwbin, '-test.run=^TestPerfHistorySnapshotGrowthDiagnostic$', '-test.count=3', '-test.v', '-test.benchtime=100ms', '-test.timeout=60s'])
    run('SaveGrowthBudget', [gwbin, '-test.run=^TestPerfHistorySaveGrowthBudget$', '-test.count=3', '-test.v', '-test.benchtime=100ms', '-test.timeout=60s'], expected=(0, 1))
    run('gateway-bench-3', [gwbin, '-test.run=^$', '-test.bench=^BenchmarkPerfHistory$', '-test.benchmem', '-test.benchtime=100ms', '-test.count=3', '-test.timeout=120s'], timeout=150)
    run('event-only-bench-3', [gwbin, '-test.run=^$', '-test.bench=^BenchmarkPerfEventOnlySave$', '-test.benchmem', '-test.benchtime=100ms', '-test.count=3', '-test.timeout=120s'], extra={'PERF_DIAG_EVENTS':'1000,10000'}, timeout=150)
    run('http-matrix-3', [httpbin, '-test.run=^TestPerfHTTPActiveHistoryMatrix$', '-test.count=3', '-test.v', '-test.timeout=120s'], timeout=150)
    run('PollWriteAmplificationGrowthBudget', [httpbin, '-test.run=^TestPerfHTTPPollWriteAmplificationGrowth$', '-test.count=3', '-test.v', '-test.timeout=60s'], expected=(0, 1))
    run('HTTPLatencyBudget', [httpbin, '-test.run=^TestPerfHTTPActiveHistoryMatrix$', '-test.count=3', '-test.v', '-test.timeout=60s'], expected=(0, 1), extra={'PERF_DIAG_EVENTS':'20000','PERF_DIAG_CLIENTS':'20','PERF_DIAG_POLLS':'2','PERF_DIAG_ENFORCE_SLO':'1'})
    if not a.skip_profiles:
        profile_n = str(min(max(counts), 20000))
        for op in ['SaveSnapshot', 'LoadSnapshot']:
            prefix = OUT / op
            run('profile-' + op, [gwbin, '-test.run=^$', '-test.bench=^BenchmarkPerfHistory/' + op + '/' + profile_n + '$', '-test.benchmem', '-test.benchtime=500ms', '-test.count=1', '-test.timeout=60s', '-test.cpuprofile=' + str(prefix) + '.cpu.pprof', '-test.memprofile=' + str(prefix) + '.heap.pprof'], extra={'PERF_DIAG_EVENTS': profile_n})
        prefix = OUT / 'HTTP'
        prefix_event = OUT / 'EventOnly'
        run('profile-event-only', [gwbin, '-test.run=^$', '-test.bench=^BenchmarkPerfEventOnlySave/10000$', '-test.benchmem', '-test.benchtime=500ms', '-test.count=1', '-test.timeout=60s', '-test.cpuprofile=' + str(prefix_event) + '.cpu.pprof', '-test.memprofile=' + str(prefix_event) + '.heap.pprof'], extra={'PERF_DIAG_EVENTS':'10000'})
        run('profile-http', [httpbin, '-test.run=^TestPerfHTTPActiveHistoryMatrix$', '-test.count=1', '-test.v', '-test.timeout=60s', '-test.cpuprofile=' + str(prefix) + '.cpu.pprof', '-test.memprofile=' + str(prefix) + '.heap.pprof', '-test.blockprofile=' + str(prefix) + '.block.pprof', '-test.blockprofilerate=1', '-test.mutexprofile=' + str(prefix) + '.mutex.pprof', '-test.mutexprofilefraction=1'], extra={'PERF_DIAG_EVENTS': profile_n, 'PERF_DIAG_CLIENTS': '40', 'PERF_DIAG_POLLS': '4'})
        for path in sorted(OUT.glob('*.pprof')):
            binary = httpbin if path.name.startswith('HTTP.') else gwbin
            for index in (['-alloc_space', '-inuse_space'] if '.heap.' in path.name else ['']):
                suffix = index.replace('-', '') or 'top'
                cmd = ['go', 'tool', 'pprof', '-top', '-nodecount=25'] + ([index] if index else []) + [str(binary), str(path)]
                run('pprof-' + path.stem + '-' + suffix, cmd)
    save()
    print('BASELINE_DIAGNOSTICS_EXECUTED; expected RED is not release PASS', flush=True)
except Exception as exc:
    save()
    print(str(exc), file=sys.stderr)
    sys.exit(2)
