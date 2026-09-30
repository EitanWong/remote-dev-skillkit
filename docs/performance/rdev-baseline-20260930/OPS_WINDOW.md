# Ops production sampling — corrected measured window

Only statistical CSV was selected from the operator-provided archive, never production state.json, credentials or a production memory profile.

Source: /srv/Projects/_inbox/rdev-quality-20260930/t_d4919217/RDEV_INCIDENT_RUN275.tar.gz / run275/BASELINE_SAMPLES.csv

Window: 2026-09-30T14:57:56.896593+08:00 -> 2026-09-30T15:07:56.739375+08:00; 21 samples; elapsed=599.842782 seconds from CSV epoch_seconds, NOT process uptime.

Measured process write_bytes increase=23150555136 B; wchar increase=23157806868 B. Mean over this exact sampled window=38594371.443 B/s (36.806 MiB/s). Earlier rough ~45MB/s estimated from21-minute uptime is superseded; cumulative start/end process counters are not interval bytes themselves.

State file size=126005903 -> 126037278 B; net size growth=31375 B. Physical process writes/net file size growth ratio=737866.3. Denominator is net file-size growth, not true changed-record bytes; process IO is not proof of fsync or exact per-file device durability.

PID unchanged across21 samples; NRestarts values=[1]. RSS nearest-rank p95=514540 KiB, max=516932 KiB in this window. This10-minute statistical window is neither60-minute soak nor proof of host/MCP task availability. Do not usehealth200/old directory entries as ACK evidence.

Operator attribution of the earlierSIGKILL is panel-manual, notOOM. Sampling begins later and is not independent cause-of-kill proof. This CSV supports prioritizing persist-frequency × bytes/write; it does not identify exact persist counts. Those counts are directly measured by our synthetic real-poll harness, reported separately with no workload-equivalence claim.
