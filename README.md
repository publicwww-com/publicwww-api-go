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

## Clusters

A cluster is a saved list of sites - the results of a search or your own list - to
pull values out of their page source or to combine with other clusters. See
[clusters in the API](https://publicwww.com/docs/api/clusters/).

```sh
publicwww cluster-create --query '"googletagmanager.com/gtm.js"'   # prints id, size, created, name
publicwww extract 12 --presets email,phone > contacts.csv          # the whole cluster, part after part
publicwww extract 12 --regex '/data-site-id="([0-9]+)"/i' > ids.csv
publicwww cluster-combine diff 14 12 --name "new this month"       # also: and, or
publicwww clusters      # and: cluster ID, presets, cluster-rename ID NAME, cluster-delete ID
```

```go
gtm, err := pw.CreateCluster(ctx, `"googletagmanager.com/gtm.js"`, "") // costs one search
mine, err := pw.CreateClusterFromDomains(ctx, []string{"example.com", "https://example.org/"}, "mine")

// Every site, in parts of up to 1000 (following NextOffset); spends extraction points.
opt := publicwww.ExtractOptions{Presets: []string{"email", "phone"}}
last, err := pw.ExtractAll(ctx, gtm.ID, opt, func(r publicwww.ExtractRow) error {
	fmt.Println(r.Domain, r.Values[0], r.Values[1]) // one list per expression
	return nil
})

hotjar, err := pw.CreateCluster(ctx, `"static.hotjar.com"`, "")
both, err := pw.CombineClusters(ctx, "and", []int{gtm.ID, hotjar.ID}, "") // "or"; "diff" - the first without the second
err = pw.ClusterDomains(ctx, both.ID, func(domain string) error {
	fmt.Println(domain)
	return nil
})
_, err = pw.DeleteCluster(ctx, hotjar.ID)
```

`ClusterPresets` lists the ready-made expressions: email, phone, whatsapp,
telegram, skype, facebook, instagram, twitter, linkedin, gtm, ga4, ua, hotjar,
adsense, bitcoin. Your own `Regex` is a PCRE in slashes; the first group is the
value. `Extract` is one part, `NextOffset` says where the next one starts. An
account keeps up to 100 clusters (`409 cluster_limit` past that). If the day's
extraction points run out midway, `ExtractAll` returns `extract_quota_exceeded`
and the last part, whose `NextOffset` is where to resume (`extract ... --offset N`).

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
