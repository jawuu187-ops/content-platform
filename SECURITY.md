# Security Policy

## Reporting a vulnerability

Report security issues privately to the repository maintainers via GitHub Security
Advisories ("Report a vulnerability" in the Security tab). Do **not** open a public issue
for a suspected vulnerability. Do not include live credentials, personal data, or
screenshots containing third-party personal data in a report.

## Response targets

| Severity | Acknowledgement | Remediation target |
| --- | --- | --- |
| Critical (funds, PII, content leak, auth bypass) | 24 hours | 72 hours |
| High | 48 hours | 7 days |
| Medium / Low | 5 days | 30 days / next release |

## Security engineering baseline

- **OWASP ASVS 5.0** is the verification baseline for all externally reachable surfaces.
- Threat model maintained in `docs/security/threat-model.md`; reviewed every release.
- All authentication is phishing-resistant where available (passkeys / WebAuthn) with
  TOTP as a fallback; SMS OTP is a last resort.
- Sensitive creator media is never served from a public bucket; access requires a
  short-lived signed token bound to the session and device.
- Screenshot/screen-recording protection is applied per-post at the creator's request.
- Payment card data never enters our scope (hosted fields / redirect only).
- Every administrative action is written to an append-only, hash-chained audit log.
- Money movements require two-person authorisation (four-eyes principle).
- Secrets are injected from GitHub Actions secrets and the deployment secret store; the
  repository contains only `.env.example` placeholders.

## Automated security gates (CI)

| Gate | Tool | Blocking |
| --- | --- | --- |
| Secret scanning | gitleaks | yes |
| Go vulnerability DB | govulncheck | yes |
| Dependency vulnerabilities | Trivy + Dependabot | yes (high/critical) |
| Dependency license policy | license report + allowlist | yes (copyleft in product paths) |
| Static analysis | `go vet`, staticcheck, Android Lint, CodeQL | yes |
| Container/image scan | Trivy | yes (high/critical) |
| DAST | OWASP ZAP baseline against staging | pre-release |

## Out of scope

Denial-of-service volume testing against production, social engineering of staff, and
physical attacks are out of scope for bounty-style reports.
