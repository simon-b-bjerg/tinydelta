# Releasing tinydelta

The [release workflow](.github/workflows/release.yml) tests the tagged
code, builds static Linux and Windows binaries for amd64 and arm64 plus
a C source archive with
[`scripts/build-release.sh`](scripts/build-release.sh), and attaches
them to the GitHub release with SHA-256 checksums. It also packs the
binaries into Python wheels with
[`python/build_wheels.py`](python/build_wheels.py) and publishes them to
PyPI, through trusted publishing (no stored token). It runs when:

- a tag of the form `vX.Y.Z` is pushed (it creates the release);
- a release is published in the GitHub UI (it adds the files to it);
- you start it by hand under Actions > Release > Run workflow and give
  an existing tag.

```sh
git tag v1.0.0 && git push origin v1.0.0
```

To try the release build without publishing anything, start it by hand
and leave the tag empty: it builds the selected branch, versioned
`dev-<commit>`, and attaches the archives to the workflow run as
`release-archives` and `python-wheels`, where you can download them. A
pull request that changes the release workflow, `scripts/build-release.sh`
or the Python package does the same automatically.

Publishing to PyPI needs a one-time setup on pypi.org: add a trusted
publisher for the project `tinydelta` with this repository, the
workflow `release.yml` and the environment `pypi`. Release tags must be
`vX.Y.Z` or `vX.Y.Z-alpha.N`/`-beta.N`/`-rc.N` (the dot is optional,
`-rcN` works too), which become Python versions `X.Y.Z` and
`X.Y.ZaN`/`bN`/`rcN`; the workflow checks the tag before it builds
anything, and publishes the archives before it builds the wheels. A tag with a suffix is
published as a pre-release, on GitHub and on PyPI, so it never becomes
the latest release and pip only installs it when asked for.
