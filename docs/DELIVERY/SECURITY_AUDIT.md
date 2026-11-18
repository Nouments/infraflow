# Security audit notes for delivery

## InfraFlow behavior exercised

- Provider and Agent used authenticated TLS connections in the isolated lab.
- Agent artifact hashes were verified before local staging.
- The successful report contained metadata and hashes, not authentication
  tokens. Repository evidence was checked for bearer-token, password/secret
  assignment, and private-key patterns; no matches were found.
- The Agent does not execute native configuration during normal synchronization.
- Test tokens were cleared from the saved GNS3 node configuration. Closing the
  project removed its stopped Docker containers. The project remains saved.
- Private lab certificate/key and token material remained outside the
  repository. Evidence excludes credentials, private keys, environment values,
  and full local filesystem paths.

## Local GNS3 host findings

The supplied GNS3 server log contained a bearer JWT. Treat that session token
as exposed and revoke it by logging out/restarting the affected session or
rotating GNS3 authentication. The local GNS3 configuration also used the
provided default credentials. Replace them with a unique strong password.

The server was configured for HTTP and logged that it listened on
`0.0.0.0:3080`. Keep the controller reachable only from trusted local users;
bind it to loopback or enforce a host firewall and TLS-protected access before
using it beyond an isolated workstation. The test did not change GNS3 server
settings or attempt to rotate credentials.

These are findings about the local test controller, not InfraFlow's Provider
or Agent implementation. The exposed JWT is not reproduced here.

## Residual InfraFlow limits

The GNS3 run did not audit every Provider/Agent production deployment setting,
third-party container layer, or host filesystem permission. No external
penetration test was performed. Do not treat successful transport or hash
verification as proof of device safety or vendor compatibility.
