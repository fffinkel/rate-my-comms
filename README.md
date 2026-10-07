# rate-my-comms

One-click rating links for emails and Slack posts. Readers click Yes, No, or a
number, the vote is counted on the spot, and they get an optional box for a
comment. No login, no second click required.

## Run

```sh
go run . -addr :8080 -dynamo-table rate-my-comms
```

| Flag | Env var | Default | Meaning |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-dynamo-table` | `DYNAMO_TABLE` | empty | DynamoDB table. When set, votes go there. |
| `-data` | `DATA_FILE` | `votes.jsonl` | Local file used when no table is set |

AWS region and credentials come from the usual SDK chain (env vars,
instance profile, pod identity). The table and an IAM policy are in
`terraform/`. Attach the policy to the role your pods assume.

Logs are JSON on stdout. `GET /healthz` returns `ok`.

## Use

Open `/links`, enter the email subject or post title, and copy the Slack, HTML
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

Results are at `/results?key=<title>`. `/results` lists every title. `/`
explains all of this to readers.

## Storage

DynamoDB holds one item per vote: partition key `key` (the title), sort key
`id`, plus `value`, `at`, and optional `comment`. Results for one title are a
single Query. The `/results` list is a Scan, fine at this volume.

Without a table the app appends votes to a JSON Lines file and replays it on
start. Good for local runs and tests. Single instance only.

## Test

```sh
go test ./...
```

The DynamoDB store test needs DynamoDB Local and is skipped otherwise:

```sh
docker run -d --rm -p 8000:8000 amazon/dynamodb-local
AWS_ENDPOINT_URL_DYNAMODB=http://localhost:8000 AWS_REGION=us-east-1 \
  AWS_ACCESS_KEY_ID=x AWS_SECRET_ACCESS_KEY=x go test ./...
```

## Known limits

- Anything that fetches the link counts as a vote, including email security
  scanners and link previews. Expect some noise, especially from Outlook
  environments.
- Results pages are open to anyone who can reach the service. Put it behind
  your SSO proxy if that matters.
- One vote per click, so one reader can vote many times.
