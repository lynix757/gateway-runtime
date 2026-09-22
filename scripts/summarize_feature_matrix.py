#!/usr/bin/env python3
import csv
import json
import pathlib
import statistics
import sys

root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else 'benchmark-results/feature-matrix')
rows = []
for path in sorted(root.glob('*.summary.json')):
    data = json.loads(path.read_text())
    metrics = data.get('metrics', {})
    dur = metrics.get('http_req_duration', {})
    reqs = metrics.get('http_reqs', {})
    failed = metrics.get('http_req_failed', {})
    profile = path.stem.replace('.summary', '')

    cpus = []
    rss_kb = []
    stats = root / f'{profile}.process.csv'
    if stats.exists():
        for line in stats.read_text().splitlines():
            parts = [p.strip() for p in line.split(',')]
            if len(parts) != 2:
                continue
            try:
                cpus.append(float(parts[0]))
                rss_kb.append(float(parts[1]))
            except ValueError:
                pass

    rows.append({
        'profile': profile,
        'rps': reqs.get('rate', 0),
        'p95_ms': dur.get('p(95)', 0),
        'avg_ms': dur.get('avg', 0),
        'failed_rate': failed.get('value', 0),
        'avg_cpu': statistics.fmean(cpus) if cpus else 0,
        'max_cpu': max(cpus) if cpus else 0,
        'avg_rss_mb': statistics.fmean(rss_kb) / 1024 if rss_kb else 0,
        'max_rss_mb': max(rss_kb) / 1024 if rss_kb else 0,
    })

if not rows:
    raise SystemExit('no summary files found')

order = {'minimal': 0, 'metrics': 1, 'trace': 2, 'access-log': 3, 'all-on': 4}
rows.sort(key=lambda r: order.get(r['profile'], 99))
baseline = next((r for r in rows if r['profile'] == 'minimal'), rows[0])
base_p95 = baseline['p95_ms'] or 0
base_rps = baseline['rps'] or 0
base_cpu = baseline['avg_cpu'] or 0

print('| profile | RPS | P95 ms | avg ms | fail | avg CPU% | max CPU% | avg RSS MiB | P95 Δ | CPU Δ |')
print('|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|')
for row in rows:
    p95 = row['p95_ms'] or 0
    p95_delta = ((p95 / base_p95) - 1) * 100 if base_p95 else 0
    cpu_delta = ((row['avg_cpu'] / base_cpu) - 1) * 100 if base_cpu else 0
    print(
        f"| {row['profile']} | {row['rps']:.2f} | {p95:.3f} | {row['avg_ms']:.3f} | "
        f"{row['failed_rate']:.4f} | {row['avg_cpu']:.2f} | {row['max_cpu']:.2f} | "
        f"{row['avg_rss_mb']:.2f} | {p95_delta:+.2f}% | {cpu_delta:+.2f}% |"
    )
