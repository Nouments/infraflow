# 04 — Security report

## SSH algorithms (fixed)

Before: x/crypto defaults, which include `diffie-hellman-group14-sha1` and `ssh-rsa`, for every device.
After: SHA-1 KEX, MACs and `ssh-rsa` signatures are excluded unless the device is listed in
`ssh_legacy_sha1_targets` as `site/device`. There is no global switch.

Device evidence:
- IOSv 15.9(3)M6 and CSR1000v IOS XE 16.07.01 offer only `diffie-hellman-group14-sha1` / `-group-exchange-sha1`
  and `ssh-rsa`. With the default policy both are refused with an explicit message (S1, X1). IOS XE 16.7 cannot be
  configured for modern KEX (`ip ssh server algorithm kex ?` lists only the two SHA-1 groups).
- With a per-device exception the session works and still negotiates `aes128-ctr` / `hmac-sha2-256` (router log).
- Revocation: removing the entry restores refusal on the next connection (unit test with a real in-process SHA-1-only server).
- CHR 7.8 negotiates without any exception.

## Host keys

Unknown keys fail closed and the error shows the presented fingerprint. In every lab run that fingerprint matched
the one derived independently from the device console (IOSv, CSR1000v, CHR). Keys exported on the CHR to read the
public key were deleted from the device afterwards. Known-hosts changes apply without a Provider restart.

## Secrets

- Lab passwords and tokens were generated per lab and stored only in a 0600 state file outside the repository and
  in `/lab/secrets.env` (0600) inside the lab container.
- Value scan: every lab secret value was searched in all tracked files and in the full diff of all commits since
  `c26fa63`: 0 matches. The gate's pattern scan (`secrets`) passes.
- Device passwords are redacted from executor errors (unit tests; F2/M10 show no secret in output).
- Packet captures are not committed (they contain TFTP payloads); only hashes and decoded summaries are.

## Dependencies

`govulncheck ./...`: no vulnerability affecting the code.

## Provisioning protocols (honest limits)

DHCP and TFTP have no authentication or integrity. A host on the bootstrap segment can answer first or serve a
different file. InfraFlow mitigations: services bind only to the configured bootstrap interface/address; TFTP
serves an allowlist of generated files with path validation; artifacts are SHA-256-verified by the Agent before
staging. The device cannot verify what it downloads. Use bootstrap only on an isolated, trusted segment, and verify
devices by read-back afterwards (done by the executors and the post-check). FTP is not used.

## Remaining risks

- SHA-1 exceptions are required for IOSv and IOS XE 16.7 devices (operator-accepted, per device).
- The GNS3 server uses default administrator credentials (lab environment, outside the repository).
- `/lab/secrets.env` and the state file must be deleted when the lab is torn down (see 07, cleanup).
