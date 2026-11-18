# Release readiness

## Delivery evidence score: 74/100

This score summarizes tested delivery evidence; it is not a security
certification or permission to release.

| Area | Score | Reason |
|---|---:|---|
| Provider–Agent appliance flow | 19/20 | Real isolated GNS3 run, TLS API access, registration, 13/13 artifact staging, and persisted report. |
| Artifact integrity and Agent state | 18/20 | Hash verification and byte comparison passed; native artifact allowlist defect fixed with tests. |
| Vendor/device compatibility | 5/15 | IOS-XE artifact generated and transferred, but no parser or Cisco device validation. |
| Security and operational safety | 9/15 | Test was isolated and credentials removed; local GNS3 uses default credentials, HTTP, wildcard bind, and a supplied log exposed a JWT. |
| QA and platform coverage | 14/20 | Full Go suite and real integration passed; race/static checks are being finalized; this revision has no Windows CI run. |
| Delivery documentation and evidence | 9/10 | Required reviews and sanitized machine-readable evidence are present. |
| **Total** | **74/100** | Evidence gaps above remain release gates. |

## Gates before broader use

- Rotate the GNS3 session token exposed in the supplied log, replace default
  GNS3 credentials, and restrict controller access or configure a protected
  endpoint. Those host changes are outside this repository pass.
- Obtain Linux and Windows CI results on the final committed source revision.
- If claiming Cisco support, run a separately authorized IOS/IOS-XE parser or
  device test and verify the resulting state. The GNS3 run documented here
  proves artifact transfer only.
- Validate Terraform, bootstrap services, and device-specific provisioning
  independently before describing them as operational.

## Source state

The Agent change consists of three Go files. No commit or push was made. GNS3
was used as external test tooling; no GNS3 client, API wrapper, or product
integration was added.
