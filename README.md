# PublicWWW API - Go

A small client for the [PublicWWW API](https://publicwww.com/docs/api/): search the
source code (HTML, JavaScript, CSS) of hundreds of millions of websites from your
own code. Standard library only.

```sh
go get github.com/publicwww-com/publicwww-api-go
go install github.com/publicwww-com/publicwww-api-go/cmd/publicwww@latest
```

## Setup

Issue a token on your [profile page](https://publicwww.com/profile/api) (a paid
plan is required for the API) and keep it in the environment, not in the code:

```sh
export PUBLICWWW_KEY=...
```

## Command line

```sh
publicwww account                          # plan and quota, spends nothing
publicwww search '"angular.min.js"' 20     # one page
publicwww export '"angular.min.js"' > sites.txt   # every url your plan covers
```

## As a package

```go
import "github.com/publicwww-com/publicwww-api-go"

pw, err := publicwww.New("")                       // key from $PUBLICWWW_KEY

page, err := pw.Search(ctx, `"angular.min.js"`, 1, 20)
fmt.Println(page.Total, "sites match")
for _, r := range page.Results {
	fmt.Println(r.Domain, r.URL)                   // r.Rank is nil for unranked sites
}

// Everything at once, streamed as NDJSON: one search, nothing held in memory.
meta, err := pw.IterAll(ctx, `"angular.min.js" site:de`, 0, func(r publicwww.Result) error {
	fmt.Println(r.Domain)
	return nil
})
if meta.Truncated {
	fmt.Println("the plan returned", meta.Returned, "of", meta.Total)
}
```

The query is the usual [PublicWWW syntax](https://publicwww.com/docs/query-syntax/):
`"exact phrase"`, several words for all of them, `-word` to exclude, `site:de`,
`depth:all` for inner pages.

## Errors

Any refusal is returned as `*publicwww.Error` with `Status` and `Code`, except one: `429 too_many_requests` (more than ten
requests a minute) is waited out using `Retry-After` and the request is repeated.
`429 quota_exceeded` is different - the day's searches are used up, retrying will
not help. All codes: [errors](https://publicwww.com/docs/api/errors/).

`PUBLICWWW_API` overrides the API address if you ever need to.

## See also

- [API documentation](https://publicwww.com/docs/api/)
- [MCP server](https://publicwww.com/docs/mcp/) - the same search for AI assistants:
  `https://api.publicwww.com/mcp`
- The same client in [Python](https://github.com/publicwww-com/publicwww-api-python), [JavaScript](https://github.com/publicwww-com/publicwww-api-javascript), [PHP](https://github.com/publicwww-com/publicwww-api-php), [Ruby](https://github.com/publicwww-com/publicwww-api-ruby)

## License

MIT
