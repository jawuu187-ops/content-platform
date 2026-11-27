# Simulated email

Status: local/demo only. No provider, DNS record or real delivery is configured.

Set `MAIL_PROVIDER=simulated` and restart the API. The identity service writes
verification, password-reset and password-changed messages to
`mail.simulated_messages`. No body is written to application logs.

Inspect the local mailbox on the database host:

```sh
DATABASE_URL='postgres://.../content_platform?sslmode=disable' \
  go run ./backend/cmd/mailsim -limit 10
```

Add `-json` to print message bodies (including codes). Treat that output as a
credential: do not paste it into a public issue or chat.

Switching to a real provider later means implementing `mail.Sender` and selecting
it in `backend/cmd/api/main.go`; identity and reset code do not need to change.
