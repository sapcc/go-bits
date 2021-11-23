# release-info

`release-info` extracts the changes for a specific version from a changelog file that uses
the [Keep a changelog](https://keepachangelog.com) format and prints the result.

We use it in our GitHub action for updating a GitHub release's description, whenever we publish a new release ([example](https://github.com/sapcc/limesctl/blob/a3c3ff1c5df528c5eef6da1b61cbf08b08705038/.github/workflows/release.yml#L34-L43)).

## Usage

```
$ release-info path-to-changelog-file vX.Y.Z
```
