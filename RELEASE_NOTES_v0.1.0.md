# codex-snapshot v0.1.0

Standalone Linux x86_64 executable for encrypted snapshots of `/home/agent/.codex`, `/home/agent/workspace`, and optional `/home/agent/.bash_history`.

- Excludes `.codex/tmp` and all `node_modules` directories.
- Produces an OpenSSL-compatible AES-256-CBC archive using PBKDF2-HMAC-SHA256 with 600,000 iterations.
- Prints the output file path, size, SHA-256, and one-time password.
- Requires no Go, tar, or OpenSSL installation to create a snapshot.

Run as a user who can read the source directories:

```sh
./codex-snapshot-linux-amd64
```

SHA-256 of release binary:

```text
3d35e0152760b767531bc90cc393154065929a9302715d3c9e433c3db03840d6  codex-snapshot-linux-amd64
```

Store the printed password separately from the encrypted archive. See README.md for decryption and restore commands. This release was cross-compiled for Linux x86_64 and tested with fixture archives and OpenSSL decryption; it has not been run on the target Debian host.
