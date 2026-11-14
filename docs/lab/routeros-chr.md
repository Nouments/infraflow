# RouterOS CHR Artifact Validation Runbook

**Current status: NOT RUN.** InfraFlow has no confirmed CHR instance or
authorized RouterOS endpoint in this work session. This procedure is for an
operator with an isolated, disposable CHR lab. It is not evidence that the
generated script currently works on a router.

## Preconditions

- An isolated CHR instance using the declared `RB750Gr3` template profile, with
  its exact RouterOS build recorded from `/system/resource/print`.
- A disposable configuration and a recovery console. Do not connect this CHR to
  production or a shared management network.
- An authorized SFTP path to this CHR. Uploading a filename ending in
  `.auto.rsc` triggers automatic execution, so review the exact file first.
- A fresh output directory and the `examples/provisioning-generation.yaml`
  input. Its TEST-NET addresses are documentation values, not live allocations.

## Procedure

1. Record the CHR version and baseline state:

   ```routeros
   /system/resource/print
   /system/identity/print
   /ip/address/print detail
   /ip/dhcp-client/print detail
   /ip/route/print detail
   ```

2. On the Provider build host, validate and generate to a new directory. For a
   PowerShell host:

   ```powershell
   go run ./provider/cmd validate -f examples/provisioning-generation.yaml
   $demo = Join-Path $env:TEMP ("infraflow-chr-" + [guid]::NewGuid())
   go run ./provider/cmd generate-all -f examples/provisioning-generation.yaml -out $demo
   Get-Content (Join-Path $demo "routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc")
   Get-FileHash (Join-Path $demo "routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc") -Algorithm SHA256
   Get-Content (Join-Path $demo "routeros-auto-rsc-demo/native/native-manifest.json")
   Get-Content (Join-Path $demo "routeros-auto-rsc-demo/site-manifest.json")
   ```

   Confirm the template ID/version, the input/output/template SHA-256 values,
   `compatibility_status: unknown`, `validation_status: render-only`, and
   `verification_status: not_performed` before transfer.

3. Review every line. Confirm the script contains no reset, Netinstall,
   identity credential, or unrelated router configuration. The `infraflow:`
   comments define which entries a repeated run may update. Do not remove these
   markers without replacing the ownership strategy.

4. Upload the reviewed file using the lab-approved SFTP client to the CHR. The
   exact transfer command depends on the operator's SFTP client and credential
   handling; do not put passwords in shell history. The filename must retain
   the `.auto.rsc` suffix. **The upload executes the script.**

5. Retrieve the matching `device-mikrotik-r1.auto.log` and preserve its actual
   bytes outside the repository. Treat any import error as a failure. Compare
   `/system/identity/print`, `/ip/address/print detail`,
   `/ip/dhcp-client/print detail`, and `/ip/route/print detail` against the
   declared intent. Check that a second run does not create duplicates.

6. Record the RouterOS build, CHR image/profile, transfer method, log hash,
   observed state, and test date. Do not promote compatibility beyond the
   tested profile/version. Remove the disposable CHR and lab credentials when
   the operator's cleanup policy requires it.

## Acceptance Evidence

Record separate results for template rendering, RouterOS import syntax, first
execution, repeated execution, resulting state, and `.auto.log` interpretation.
Passing Go tests or matching SHA-256 values proves deterministic file
integrity only; it does not satisfy any CHR or hardware validation result.