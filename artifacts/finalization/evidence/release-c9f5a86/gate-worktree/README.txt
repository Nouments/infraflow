Gate on the working tree = c9f5a86 + uncommitted changes (D13 log-store fix, Cisco persist, soak mode, lab topologies).
context.txt lists the dirty files and the diff SHA-256 at gate start.
Result: 12/13 PASS; web-e2e FAILED at W08 when Chrome's renderer died ("Unable to receive message from renderer");
every later check failed on the closed browser session. The host was under memory pressure at the time (GNS3 VMs,
a 3 h soak, the race step; 5.6 GiB swap in use). After stopping a lab VM, web-e2e was re-run alone with the same
gate-built Provider: 16/16 PASS (web-e2e-rerun.txt). The first run is kept; it is not counted as a pass.
