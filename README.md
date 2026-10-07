# rate-my-comms

One-click rating links for emails and Slack posts. Readers click Yes, No, or a
number, the vote is counted on the spot, and they get an optional box for a
comment. No login, no second click required.

## Run

```sh
go run . -addr :8080 -data votes.jsonl
```

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_FILE` | `votes.jsonl` | Where votes are stored |

Logs are JSON on stdout. `GET /healthz` returns `ok`.

## Use

Open `/`, enter the email subject or post title, and copy the Slack, HTML
email, or plain text snippet into your message. Or write the links by hand:

```
https://rate.example.com/vote?key=Weekly%20update&value=yes
https://rate.example.com/vote?key=Weekly%20update&value=no
https://rate.example.com/vote?key=Weekly%20update&value=4
```

Any GET to `/vote` counts one vote and shows the reader an optional comment
box. `value` is `yes`, `no`, or an integer from 1 to 10. Use `%20` for spaces
in the key. Generated links use the request host; set `X-Forwarded-Proto`
on your proxy for https.

Results are at `/results?key=<title>`. `/results` lists every title.

## Storage

Votes append to a JSON Lines file, one record per line. Adding a comment
appends a second record with the same `id`; the file is replayed on start and
later records win. Back it up by copying the file. Run one instance per file.

## Known limits

- Anything that fetches the link counts as a vote, including email security
  scanners and link previews. Expect some noise, especially from Outlook
  environments.
- Results pages are open to anyone who can reach the service. Put it behind
  your SSO proxy if that matters.
- One vote per click, so one reader can vote many times.
