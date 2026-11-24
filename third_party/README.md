# Preserved sources

These are checked-in source archives, not Git LFS pointers or network redirects.
`sources.lock.json` pins their bytes and module manifests:

- `airsim-agent-base.tar.gz`: unmodified Agent subset exported with `git archive`
  at `patches/AIRSIM_BASE`; includes upstream LICENSE, NOTICE and THIRD_PARTY_NOTICES.
  Apply `patches/AirSIM-mu300.patch` only in an isolated directory.
- `plugin-go-vendor.tar.gz` and `agent-go-vendor.tar.gz`: `go mod vendor` outputs
  from the exact module manifests. All generated dependency notices are retained.
  They use their individual upstream licenses, not the integration license.

`scripts/preserved-sources.py` verifies hashes before extraction and rejects links,
path traversal, changed module manifests and overwriting existing files. Updating
an Agent commit or a Go dependency requires reviewing and replacing its preserved
archive and lock together; there is no silent fallback to upstream HEAD.

`recovery.lock.json` pins larger build/installation inputs held outside Git in a
dependency vault. It does not itself contain those files. See
[`docs/OFFLINE_RECOVERY.md`](../docs/OFFLINE_RECOVERY.md) for capture, restore and
the distinction between repository-contained sources and the separate vault.
