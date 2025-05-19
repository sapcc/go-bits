<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# release-info

`release-info` extracts and prints the changes for a specific version from a changelog file that uses the [keep a changelog](https://keepachangelog.com/en/1.1.0/) format.

Although `release-info` is designed for working with "keep a changelog" format, you don't necessarily need to adhere to it entirely.

The only hard requirement for `release-info` is the heading format for different versions. `release-info` expects the heading for a specific version in the following format:

```
## X.Y.Z - YEAR-MONTH-DATE
```

> [!NOTE]
> You can also prefix the version with `v`, e.g. `vX.Y.Z`. However, for simplicity it is recommended that you omit the prefix.

You can use any arbitrary format for your changelog as long as you use the above heading format for versions.

## Example

The following is an example of what a minimal changelog file could look like:

```
## 2.0.0 - 2022-10-01

- Added some breaking changes.

...

## 1.0.1 - 2021-05-13

- Fixed a bug.

## 1.0.0 - 2021-05-10

Initial release
```

## Usage

```
$ go install github.com/sapcc/go-bits/tools/release-info@latest
$ release-info path-to-changelog-file vX.Y.Z
```
