# Email delivery

Account verification needs a real outbound mail path. The API supports two
providers behind `internal/platform/mail.Sender`:

- `smtp` — direct SMTP with TLS; refused unless TLS is available.
- `brevo` — Brevo transactional email HTTPS API.

The local `log` sender remains for development only and is refused outside
local by configuration validation when no real provider is present.

## Why HTTPS

The UpCloud trial host blocks outbound TCP 25/465/587/2525. SMTP therefore
cannot connect from this host. HTTPS on 443 works, so an HTTPS email API is the
only viable route without changing hosting or asking the provider to lift an
egress restriction.

## Provider evaluation

Checked August 2026. Free means no card and no paid commitment.

| Provider | Free allowance | Sender/domain | DNS needed to start | Notes |
|---|---|---|---|---|
| Brevo | 300/day forever | single sender verified by email, or authenticated domain | No for a verified single sender | API key, HTTPS `POST /v3/smtp/email`; free emails carry Brevo branding |
| Resend | 3,000/month, 100/day | custom domain verification required for a domain sender | Yes | Without DNS only `onboarding@resend.dev` can send, so it cannot preserve `info@remixofficial.online` |
| SMTP2GO | 1,000/month, 200/day | single sender or sender domain | No for a single sender, but 25/hour until a domain is verified | New accounts can be manually reviewed |
| Mailjet | 6,000/month, 200/day | single sender or domain | No for a single sender | Free plan branding |
| Zoho Mail API | Existing mailbox quota | existing domain | No | Requires an OAuth client and refresh token; free-plan API availability is not guaranteed by the web-only free plan |

Current domain mail records: Zoho MX, SPF includes `zohomailcloud.co.uk`,
DMARC `p=none`. Because DMARC is not enforcing, a Brevo single-sender message is
not blocked outright, but it is not SPF-aligned until Brevo's DNS records are
added. Domain authentication is recommended once DNS changes are approved; it
is not required to make the first verified send.

## Selected provider

**Brevo.** Free, no card, HTTPS-only, and it can verify `info@remixofficial.online`
by sending a confirmation link to that existing Zoho mailbox. Sender identity
therefore stays `info@remixofficial.online`.

Configuration, once the user has created the account and verified the sender:

```
MAIL_PROVIDER=brevo
MAIL_FROM=info@remixofficial.online
BREVO_API_KEY=<server-side secret only>
```

`BREVO_API_KEY` must never be committed, pasted into chat, or logged.

## What was verified locally

- `mail.BrevoSender` sends the expected JSON to the configured endpoint and
  authenticates with the `api-key` header.
- A non-2xx response is surfaced as an error.
- Configuration accepts `MAIL_PROVIDER=brevo` with a key and a real sender
  outside local, without requiring SMTP.
- `go test ./...` passes.

Live delivery has not been tested because no Brevo account or API key exists
yet. This document must not be read as evidence that mail was delivered.
