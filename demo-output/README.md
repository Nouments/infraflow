# Demo output and equipment references

This directory contains generated examples and the source-backed equipment notes used to plan the next template-generation pass. Generated text is not proof of syntax acceptance, device compatibility, deployment, or device state.

The current Cisco output remains at `demo/R1/config.cfg`, with its generation report at `demo/generation-report.json`. The report marks it `rendered_unverified`; the interface name and address are demo input, not verified ISR 1100 hardware facts.

See [equipment/README.md](equipment/README.md) for the pilot inventory, official references, and per-vendor generation boundaries. No vendor datasheet PDF is redistributed here; the notes link to the original sources.

## Multi-vendor generation pilot

The input fixture is [pilot-input.yaml](pilot-input.yaml). It declares three demo devices with documentation-only addresses; `ether2`, `port2`, and the generic FortiGate model are fixture values, not verified hardware facts.

Reproduce the existing generator output with:

```sh
go run ./provider/cmd generate-all -f demo-output/pilot-input.yaml -out demo-output/pilot-generated
```

The [generation report](pilot-generated/vendor-pilot/generation-report.json) records the current hash-verified artifacts. Generation does not start bootstrap services, validate Terraform, or contact devices.

This pilot exercises native text rendering for Cisco IOS XE, MikroTik RouterOS 7, and FortiOS. It does **not** emit RouterOS `.rsc`/`.auto.rsc` or FortiDeploy/FortiManager ZTP artifacts, nor does it provide a complete ZTP/AutoInstall workflow. Those are the provisioning focus, gated on exact release/model and backend inputs.

## Pilot order

1. Keep the existing Cisco `.cfg` render as the baseline.
2. Select an exact MikroTik model and RouterOS release before producing separate RouterOS 6/7 `.rsc` and `.auto.rsc` candidates.
3. Select the FortiGate model and ZTP backend. FortiDeploy Cloud and FortiManager DHCP-option ZTP are separate workflows, not interchangeable local configuration files.
4. Keep every generated artifact `render-only` until syntax and target-device checks are recorded for that exact model and software release.