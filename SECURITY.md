# Security

UGT exists to run code from repositories the user did not write. Isolation
failures, ways for a repository or recipe to reach the user's files or
credentials, and ways to get an unverified image run as a verified one are all in
scope.

## Reporting

Report vulnerabilities privately through GitHub's private vulnerability reporting
on this repository ("Report a vulnerability" under the Security tab). Do not open
a public issue. Include the affected component, steps to reproduce and the impact
you observed.

## Boundaries UGT relies on

- Project code runs in containers, inside a Linux VM on Windows and macOS.
- Project containers never receive the user's home directory, credentials or the
  container engine socket.
- Services bind to `127.0.0.1` only.
- Registry images are built by CI from reviewed recipes; the helper verifies their
  attestations before running them.
- Repository content is treated as untrusted data. It never grants an agent
  broader permissions.

A container inside a VM is a strong boundary, not a perfect one. Treat any path
around these boundaries as a vulnerability.
