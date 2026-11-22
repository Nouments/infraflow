# 06 — Failures and blockers

## Defects found and fixed during this mission

| ID | Found by | Defect | Fix commit | Regression test |
|---|---|---|---|---|
| D0 | GNS3 two-Agent run | Agents on one host shared one log outbox; one rejected batch blocked every later run of every Agent | 5a180f5 | outbox_test.go, config_test.go |
| D-SITE | GNS3 inventory | A Cisco IOS device with `method: ssh` made generation fail for its whole site | 6edeb5c | native_test.go |
| D-ROLL | Review + IOSv | No rollback after a partial apply | 9932eca | cisco_ios_rollback_test.go |
| D-LOCK | Review | Two executions could configure one device at once | 9932eca | device_lock_test.go (mutation-checked) |
| D-SHA1 | Review | SHA-1 SSH algorithms enabled for every device | 9932eca | ssh_policy_test.go |
| D1 | CHR 7.8 | auto-rsc added the day-0 management address unconditionally → "already have such address" | 30711a1 | native_test.go |
| D2 | CHR 7.8 | `/import verbose=no` over SSH prints nothing and exits 0 on failure | 2f35012 | TestRouterOSImportTreatsSilentImportAsFailure |
| D3 | CHR 7.8 | Unquoted prefixes in `find where` never match | 30711a1 | native_test.go |
| D4 | CHR 7.8 | `verbose=yes` never prints a success line | 2f35012 | fake server mirrors device |
| D5 | CSR1000v 16.7 | `end` at an EXEC prompt timed out and closed the session; rollback could not run | 1c20d22 | TestSSHSessionEndAtExecPromptIsNotSent |
| D6 | Browser E2E | Web plan-and-generate refused RouterOS auto-rsc and Cisco IOS | 93a7ebc | TestPlanAndGenerateAgreesWithNativeGenerator |
| D7 | Diagnostics on real capture | A TFTP transfer cut by the end of a capture was reported as failed | dd7b140 | TestCaptureReportsInterruptedTFTPTransfer |
| D8 | Hosted CI (Windows) | RouterOS unreachable test asserted Linux error text | aa79e52 | itself |
| D9 | Load profile (2026-11-22) | Every single-artifact read re-hashed every artifact of every site: one `/api/v1/sites` = 1 + 2×sites full catalog passes, an Agent sync of N files = N passes | uncommitted | TestReadArtifactVerifiesOnlyItsOwnSite |
| D10 | Load profile (2026-11-22) | Central log store re-parsed every stored timestamp on every append (retention purge) | uncommitted | TestStoreRetentionCacheNeverHidesAnExpiredEvent (mutation-checked) |
| D11 | Review for AGT-11 (2026-11-22) | A partial or empty Agent report was shown as "N completed, 0 failed of N" (total = results in the report, not published artifacts); rejected reports left no trace | uncommitted | TestReportOutcomeClassification, TestRejectedReportsAreRecordedAndDuplicatesAreNotDoubleCounted |
| D12 | Real GNS3 run (2026-11-22) | New GNS3 client: a template with `"cpus": 0.0` broke decoding and a partially decoded list was used | uncommitted (new code) | TestUndecodableListReturnsNothingRatherThanAPartialList, TestInventoryMarksTemplatesWhoseImageIsAbsent |

## Blockers

| ID | Item | Evidence | What unblocks it |
|---|---|---|---|
| B1 | FortiGate FGT1 delivery/verification | fortinet/fgt1-blocked.txt | the appliance's admin password or a fresh FortiOS VM; then a FortiOS executor is needed |
| B2 | IOL-XE 17.18 | `docker images` has no `iol-xe` image | the licensed image |

## Open (not defects in InfraFlow code, or not resolved)

| Item | Status | Facts |
|---|---|---|
| IOSv AutoInstall (M6, M8) | M6: UNSUPPORTED (2026-11-22); M8: PARTIAL | 2026-11-20: `DHCP_OFFER_NOT_ACCEPTED` on the wire, cause unproven. 2026-11-22: same behaviour with a reference ISC dhcpd (3 option sets); IOS trace shows the AutoInstall handler stopping in 0 ms; the regular DHCP client binds. Image-side. See gns3-20261122/CIS-02-REF.json |
| CHR factory DHCP client | observation | not removed by the generated script; OSPF sourced from that address |
| Web console example | UX | built-in example has no provisioning method, so "Generate" returns NOT_REQUIRED |
| GNS3 CSR1000v vmxnet3 | lab | link stays down; virtio-net-pci works |
| Interrupted Agent sync, incomplete report, load | PASS (2026-11-22) | AGT-10, AGT-11, PRV-19 in 05 |
| Reload persistence (GNS-04), multi-hour soak (PRV-20), GNS3 view in the Admin Web (GNS-09) | NOT_RUN | executors do not write startup config; only 10 s load stages; GNS3 is CLI lab tooling |
| GNS3 category in hosted CI | NOT_RUN | needs a lab runner and a token with `workflow` scope |
