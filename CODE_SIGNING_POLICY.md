# Code signing policy

Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).

## Team roles

| Role | Members |
|---|---|
| Committers and reviewers | [Italo Mesquita](https://github.com/italombs) |
| Approvers | [Italo Mesquita](https://github.com/italombs) |

## What is signed

Only `ParamLens.exe`, built by the GitHub Actions workflow
[`.github/workflows/release.yml`](.github/workflows/release.yml) from a tagged commit of this repository.
Every signing request is manually approved by an approver.

## Privacy policy

This program will not transfer any information to other networked systems unless specifically
requested by the user or the person installing or operating it.

ParamLens only connects to the SQL Server instance the user configures, and serves its interface
on `127.0.0.1` (loopback) protected by a per-session token. It has no telemetry, does not check for
updates and does not contact any other service.
