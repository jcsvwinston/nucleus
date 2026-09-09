---
sidebar_position: 4
title: Verifying a release
description: Check that a downloaded Nucleus archive or container image is the one this repository's release workflow built, using cosign and gh attestation.
covers: []
config_keys: []
---

# Verifying a release

A GitHub release page is not a chain of custody. Anyone with write access —
or anyone who takes it — can replace an asset with a binary that runs just
as happily as the real one. Nucleus releases cut from the first signed tag
onward therefore publish two independent proofs of where their archives came
from, and this page is how you consume them.

Signing cannot be applied retroactively — a release is signed by the run that
builds it, with a certificate minted for that run — so releases cut before
signing was added publish at most their archives and a `checksums.txt`, and
many of them publish no assets at all (every release from v1.0.0 to v1.15.1,
and several as late as v1.22.0). Look at the release page before you start: a
signed release lists `checksums.txt.sig`; one that lists archives and a
checksum file can be checked for integrity with `sha256sum -c` but has no
origin to verify; one with an empty asset list gives you neither.

A signed release carries:

| Asset | What it is |
| --- | --- |
| `nucleus_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) | The CLI archive, six in total. |
| `<archive>.spdx.json` | An SPDX SBOM, one per archive, listing what went into it. |
| `checksums.txt` | SHA-256 of every archive and every SBOM. |
| `checksums.txt.sig` / `checksums.txt.pem` | A keyless signature over `checksums.txt`, and the short-lived certificate that made it. |

Releases that publish a container image sign and attest it in the same run
and against the same identity; the image lives at
`ghcr.io/jcsvwinston/nucleus` rather than on the release page, and
[section 4](#4-verify-the-container-image) covers it.

There is no long-lived signing key to trust, and none is published. The
signature is made by the release workflow itself with a certificate minted
for that single run, and what you check is not "who holds the key" but
**which workflow, in which repository, at which tag** produced the release.
A build provenance attestation, stored by GitHub rather than on the release
page, records the same thing a second way.

## Before you start

Install [cosign](https://docs.sigstore.dev/cosign/system_config/installation/)
and the [GitHub CLI](https://cli.github.com/). Then set the release you are
verifying:

```bash
export TAG=vX.Y.Z             # the signed release you downloaded
export VERSION=${TAG#v}
export PKG=nucleus_${VERSION}_linux_amd64.tar.gz
```

## 1. Verify the signature over the checksum file

Download the checksum file, its signature and its certificate, then check
them:

```bash
gh release download "$TAG" --repo jcsvwinston/nucleus \
  --pattern checksums.txt \
  --pattern checksums.txt.sig \
  --pattern checksums.txt.pem

cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/jcsvwinston/nucleus/.github/workflows/release.yml@refs/tags/${TAG}"
```

`Verified OK` means the checksum file was produced by
`.github/workflows/release.yml` in `jcsvwinston/nucleus`, running at the tag
you named, and has not been altered since.

:::caution The identity ends at the tag, not at `main`
Nearly every cosign example on the internet ends the identity in
`@refs/heads/main`. That string verifies nothing here. The release workflow
is dispatched at the **tag** ref, so the certificate it signs with names
`@refs/tags/<tag>` — substitute the tag you are verifying, exactly as the
command above does. An identity ending in `refs/heads/main` will fail with
`none of the expected identities matched`, and that failure is correct.
:::

If you verify releases from a script and would rather not rewrite the
identity for each tag, match the shape instead of the value:

```bash
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/jcsvwinston/nucleus/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$'
```

Keep the anchors and keep `refs/tags/` in the pattern. A regexp loose enough
to match any ref accepts a signature made from any branch of the repository,
which is most of the guarantee gone.

## 2. Carry the proof across to your archive

The signature covers `checksums.txt`. The checksum file covers the archive,
so one more step moves the trust onto the file you are about to run:

```bash
gh release download "$TAG" --repo jcsvwinston/nucleus --pattern "$PKG"
awk -v f="$PKG" '$2 == f' checksums.txt | sha256sum -c -
```

On macOS, where there is no `sha256sum`, the last command is
`awk -v f="$PKG" '$2 == f' checksums.txt | shasum -a 256 -c -`.

The SBOM beside each archive is listed in the same signed checksum file, so
the same two commands verify it: append `.spdx.json` to `PKG` and run them
again.

## 3. Verify the build provenance

The second, independent proof does not use the release assets at all: it
asks GitHub which workflow run built the file in front of you.

```bash
gh attestation verify "$PKG" \
  --repo jcsvwinston/nucleus \
  --signer-workflow jcsvwinston/nucleus/.github/workflows/release.yml
```

This matches the archive by digest against the attestation the release job
wrote, and `--signer-workflow` is the part that matters: it refuses an
attestation signed by any other workflow, in any other repository. The
command reads public data and needs no credentials beyond a logged-in
`gh`.

## 4. Verify the container image

The image at `ghcr.io/jcsvwinston/nucleus` is pushed, signed and attested by
the same job, in the same run, as the archives — so the identity to check is
the one you already used. Both commands take a tag and resolve it themselves
to the digest of the multi-architecture index, which is what was signed:

```bash
export IMAGE=ghcr.io/jcsvwinston/nucleus:${VERSION}

cosign verify "$IMAGE" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity "https://github.com/jcsvwinston/nucleus/.github/workflows/release.yml@refs/tags/${TAG}"
```

The tag-not-`main` caution above applies here unchanged, and so does the
regexp form if you verify from a script — only the subject changes, from
`checksums.txt` to the image reference.

The provenance attestation is checked the same way as the archive's, with an
`oci://` subject:

```bash
gh attestation verify "oci://$IMAGE" \
  --repo jcsvwinston/nucleus \
  --signer-workflow jcsvwinston/nucleus/.github/workflows/release.yml
```

Both commands read the registry, so an image that is not public needs a
`docker login ghcr.io` first. Resolve the tag to a digest once
(`docker buildx imagetools inspect "$IMAGE"`) and verify
`ghcr.io/jcsvwinston/nucleus@sha256:…` instead if you want the check pinned
to exactly the image you are about to run — a tag can be moved, a digest
cannot.

## When verification fails

- **`no matching signatures` or `none of the expected identities matched`** —
  the identity string is the first thing to check, and the tag inside it the
  first part of that. See the note above.
- **The release publishes no `checksums.txt.sig`** — releases cut before
  signing was introduced carry at most archives and a checksum file, and many
  carry no assets at all. Where a `checksums.txt` is published the archives
  can still be checked for integrity with `sha256sum -c`, but there is
  nothing to verify their origin against; upgrade to a release that publishes
  a signature.
- **`gh attestation verify` reports no attestation** — same reason, same
  answer.
- **The image tag does not exist** — releases cut before the image pipeline
  was added publish archives only, and a prerelease publishes its version tag
  without moving `latest`. The repository's Packages page lists what is there.

Report anything that verifies against an identity other than the one above,
or an archive whose digest is absent from a validly signed checksum file,
through the repository's security policy rather than a public issue.
