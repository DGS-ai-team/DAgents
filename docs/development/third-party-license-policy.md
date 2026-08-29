# Third-party license policy

DAgents is distributed under the MIT license. Dependencies shipped in the
Node Web UI and Manage Console must have a permissive license compatible with
that distribution model. The CI policy currently allows:

- MIT, ISC, 0BSD, BlueOak-1.0.0
- Apache-2.0
- BSD-2-Clause and BSD-3-Clause
- MPL-2.0 when combined with Apache-2.0 as declared by the package

GPL, AGPL, LGPL, SSPL, source-available, and packages with missing or unclear
license metadata require an explicit maintainer/legal review before merging.
Do not solve a license failure by changing the allowlist without recording the
package, version, license text, distribution impact, and approval in the PR.

The CI license check scans installed package metadata for all three shipped
Vue applications. `package-lock.json` remains the version lock, `npm audit`
checks known npm vulnerabilities, and Go dependencies are checked with
`govulncheck`; new Go or Python dependencies still require a human license
review in the PR because their licenses are not inferred from module names.

## Dependency audit exception

As of 2026-08-29, `browser-use==0.13.8` pins `click==8.3.1`,
`mcp==1.26.0`, and `pypdf==6.14.2`, while the fixed versions are newer than
those exact upstream pins. The PR audit keeps the six currently known IDs
(`PYSEC-2026-2132`, `PYSEC-2026-3481`, `PYSEC-2026-3482`,
`PYSEC-2026-3483`, `PYSEC-2026-3655`, and `PYSEC-2026-3656`) explicit so a new
finding still fails CI. This is an upstream-blocked exception, not a security
approval; review it by 2026-09-30 and remove it as soon as a released
browser-use version accepts the fixed dependencies. The maintainer fallback
for that review is `@AphroditeOotsuka`.
