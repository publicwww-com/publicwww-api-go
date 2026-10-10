// Package publicwww is a small client for the PublicWWW API -
// https://publicwww.com/docs/api/ - standard library only.
//
//	pw, err := publicwww.New("")                     // key from $PUBLICWWW_KEY
//	page, err := pw.Search(ctx, `"angular.min.js"`, 1, 20)
//	err = pw.IterAll(ctx, `"angular.min.js"`, 0, func(r publicwww.Result) error {
//		fmt.Println(r.Domain)
//		return nil
//	})
//
//	c, err := pw.CreateCluster(ctx, `"googletagmanager.com/gtm.js"`, "")
//	opt := publicwww.ExtractOptions{Presets: []string{"email", "phone"}}
//	last, err := pw.ExtractAll(ctx, c.ID, opt, func(r publicwww.ExtractRow) error {
//		fmt.Println(r.Domain, r.Values)
//		return nil
//	})
package publicwww

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const userAgent = "publicwww-api-go/1.1"

// Result is one site. Rank is nil when the site has no rank.
type Result struct {
	Domain   string   `json:"domain"`
	URL      string   `json:"url"`
	Rank     *int     `json:"rank"`
	Ranked   bool     `json:"ranked"`
	Snippets []string `json:"snippets,omitempty"`
}

// Page is one page of search results.
type Page struct {
	Query      string   `json:"query"`
	Page       int      `json:"page"`
	PerPage    int      `json:"per_page"`
	Total      int      `json:"total"`
	TotalPages int      `json:"total_pages"`
	Returned   int      `json:"returned"`
	Truncated  bool     `json:"truncated"`
	TookMs     int      `json:"took_ms"`
	Results    []Result `json:"results"`
}

// Meta is the first line of an NDJSON answer.
type Meta struct {
	Total     int  `json:"total"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`
}

// Account is the plan, quota and limits behind the key.
type Account struct {
	Plan       string `json:"plan"`
	FullAccess bool   `json:"full_access"`
	Quota      struct {
		Searches struct {
			Limit    int   `json:"limit"`
			Used     int   `json:"used"`
			ResetsAt int64 `json:"resets_at"`
		} `json:"searches"`
	} `json:"quota"`
	Limits struct {
		MaxPerPage int `json:"max_per_page"`
	} `json:"limits"`
	Clusters struct {
		Count               int    `json:"count"`
		Max                 int    `json:"max"`
		MaxSize             int    `json:"max_size"`
		ExtractPointsPerDay int    `json:"extract_points_per_day"`
		ExtractPointsLeft   int    `json:"extract_points_left"`
		ResetsAt            string `json:"resets_at"`
	} `json:"clusters"`
}

// Cluster is a saved list of sites. Page, PerPage, TotalPages and Domains
// are filled by Client.Cluster - one page of its domains.
type Cluster struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Size       int      `json:"size"`
	Created    string   `json:"created"`
	Page       int      `json:"page,omitempty"`
	PerPage    int      `json:"per_page,omitempty"`
	TotalPages int      `json:"total_pages,omitempty"`
	Domains    []string `json:"domains,omitempty"`
}

// ClusterList is the account's clusters, newest first.
type ClusterList struct {
	Count    int       `json:"count"`
	Max      int       `json:"max"`
	Clusters []Cluster `json:"clusters"`
}

// Preset is a ready-made extraction expression.
type Preset struct {
	Name  string `json:"name"`
	Regex string `json:"regex"`
	About string `json:"about"`
}

// ExtractOptions says what to extract and from which part of the cluster.
type ExtractOptions struct {
	Presets      []string // ready-made expressions by name: "email", "phone", ...
	Regex        []string // your own PCRE in slashes, the first group is the value
	Offset       int      // where to start: NextOffset of the previous part
	Limit        int      // domains per part, up to 1000; 0 means 1000
	IncludeEmpty bool     // also list sites where nothing matched
}

// ExtractRow is one site: Values holds one list per expression.
type ExtractRow struct {
	Domain  string     `json:"domain"`
	Values  [][]string `json:"values"`
	Matches int        `json:"matches"`
}

// Extraction is one part of an extraction. NextOffset is nil when the
// cluster is done.
type Extraction struct {
	Cluster     int          `json:"cluster"`
	Name        string       `json:"name"`
	Size        int          `json:"size"`
	Offset      int          `json:"offset"`
	Scanned     int          `json:"scanned"`
	InIndex     int          `json:"in_index"`
	WithMatches int          `json:"with_matches"`
	NextOffset  *int         `json:"next_offset"`
	Regex       []string     `json:"regex"`
	PointsUsed  float64      `json:"points_used"`
	PointsLeft  int          `json:"points_left"`
	Stopped     string       `json:"stopped,omitempty"`
	Rows        []ExtractRow `json:"rows"`
}

// Deleted is the answer to DeleteCluster.
type Deleted struct {
	Deleted int    `json:"deleted"`
	Name    string `json:"name"`
}

// Error is an error answer from the API.
type Error struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

// Client talks to the API.
type Client struct {
	Key  string
	API  string
	HTTP *http.Client
}

// New returns a client; an empty key is taken from $PUBLICWWW_KEY.
func New(key string) (*Client, error) {
	if key == "" {
		key = os.Getenv("PUBLICWWW_KEY")
	}
	if key == "" {
		return nil, &Error{Code: "missing_key", Message: "set PUBLICWWW_KEY or pass a key"}
	}
	api := os.Getenv("PUBLICWWW_API")
	if api == "" {
		api = "https://api.publicwww.com"
	}
	return &Client{Key: key, API: strings.TrimRight(api, "/"), HTTP: &http.Client{Timeout: 10 * time.Minute}}, nil
}

// get performs a GET - or a POST of body as JSON when body is not nil - and
// returns a 2xx response. It waits out the rate limit; any other error is
// returned. The caller closes the body.
func (c *Client) get(ctx context.Context, path string, params url.Values, body any) (*http.Response, error) {
	u := c.API + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	method, data := "GET", []byte(nil)
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return nil, err
		}
		method = "POST"
	}
	for {
		var rd io.Reader
		if data != nil {
			rd = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("User-Agent", userAgent)
		if data != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res, nil
		}

		var body struct {
			Error struct {
				Code       string `json:"code"`
				Message    string `json:"message"`
				RetryAfter int    `json:"retry_after"`
			} `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()

		// Two different 429s: too_many_requests means "slow down and repeat",
		// quota_exceeded means "come back tomorrow".
		if res.StatusCode == 429 && body.Error.Code == "too_many_requests" {
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if wait == 0 {
				wait = body.Error.RetryAfter
			}
			if wait == 0 {
				wait = 10
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		code := body.Error.Code
		if code == "" {
			code = "http_error"
		}
		return nil, &Error{Status: res.StatusCode, Code: code, Message: body.Error.Message}
	}
}

// call decodes the JSON answer of get into out.
func (c *Client) call(ctx context.Context, path string, params url.Values, body, out any) error {
	res, err := c.get(ctx, path, params, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(out)
}

// Account returns the plan, quota and limits, clusters included. It spends
// nothing.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	var a Account
	return &a, c.call(ctx, "/v1/account", nil, nil, &a)
}

// Search returns one page of results.
func (c *Client) Search(ctx context.Context, query string, page, perPage int) (*Page, error) {
	var p Page
	return &p, c.call(ctx, "/v1/search", url.Values{
		"query": {query}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
	}, nil, &p)
}

// IterAll streams every row of a query the plan covers as NDJSON - one
// search, nothing held in memory - and calls fn for each. perPage 0 means the
// plan's max_per_page (asking for more is a 400 per_page_too_large). The
// returned Meta says whether the plan cut the result short.
func (c *Client) IterAll(ctx context.Context, query string, perPage int, fn func(Result) error) (*Meta, error) {
	if perPage == 0 {
		a, err := c.Account(ctx)
		if err != nil {
			return nil, err
		}
		perPage = a.Limits.MaxPerPage
	}
	res, err := c.get(ctx, "/v1/search", url.Values{
		"query": {query}, "per_page": {strconv.Itoa(perPage)}, "format": {"ndjson"},
	}, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	meta := &Meta{}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024) // a row with snippets can be long
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if first { // the metadata line
			first = false
			if err := json.Unmarshal(line, meta); err != nil {
				return nil, err
			}
			continue
		}
		var r Result
		if err := json.Unmarshal(line, &r); err != nil {
			return meta, err
		}
		if err := fn(r); err != nil {
			return meta, err
		}
	}
	return meta, sc.Err()
}

// ---- clusters: saved lists of sites, to extract data from or combine ----

// Clusters returns the account's clusters, newest first.
func (c *Client) Clusters(ctx context.Context) (*ClusterList, error) {
	var l ClusterList
	return &l, c.call(ctx, "/v1/clusters", nil, nil, &l)
}

// Cluster returns one cluster and a page of its domains (perPage up to 10000).
func (c *Client) Cluster(ctx context.Context, id, page, perPage int) (*Cluster, error) {
	var cl Cluster
	return &cl, c.call(ctx, "/v1/clusters/"+strconv.Itoa(id), url.Values{
		"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
	}, nil, &cl)
}

// ClusterDomains calls fn for every domain of a cluster, page after page.
func (c *Client) ClusterDomains(ctx context.Context, id int, fn func(string) error) error {
	for page := 1; ; page++ {
		cl, err := c.Cluster(ctx, id, page, 10000)
		if err != nil {
			return err
		}
		for _, d := range cl.Domains {
			if err := fn(d); err != nil {
				return err
			}
		}
		if page >= cl.TotalPages {
			return nil
		}
	}
}

// CreateCluster makes a cluster from the results of a search; it costs one
// search, like Search. An empty name means the query. 409 cluster_limit when
// the account has 100 clusters.
func (c *Client) CreateCluster(ctx context.Context, query, name string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.call(ctx, "/v1/clusters", nil, map[string]any{"query": query, "name": name}, &cl)
}

// CreateClusterFromDomains makes a cluster from domains or urls; only sites
// in the index are kept.
func (c *Client) CreateClusterFromDomains(ctx context.Context, domains []string, name string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.call(ctx, "/v1/clusters", nil, map[string]any{"domains": domains, "name": name}, &cl)
}

// CombineClusters makes a new cluster: "and" - domains in every cluster,
// "or" - in any, "diff" - in the first but not in the second (exactly two).
func (c *Client) CombineClusters(ctx context.Context, operation string, ids []int, name string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.call(ctx, "/v1/clusters/combine", nil,
		map[string]any{"operation": operation, "clusters": ids, "name": name}, &cl)
}

// ClusterPresets returns the ready-made extraction expressions.
func (c *Client) ClusterPresets(ctx context.Context) ([]Preset, error) {
	var res struct {
		Presets []Preset `json:"presets"`
	}
	err := c.call(ctx, "/v1/clusters/presets", nil, nil, &res)
	return res.Presets, err
}

// Extract runs one part of an extraction: domains [Offset, Offset+Limit) of
// the cluster. It costs extraction points.
func (c *Client) Extract(ctx context.Context, id int, opt ExtractOptions) (*Extraction, error) {
	if opt.Limit == 0 {
		opt.Limit = 1000
	}
	body := map[string]any{
		"presets": append([]string{}, opt.Presets...), "regex": append([]string{}, opt.Regex...),
		"offset": opt.Offset, "limit": opt.Limit, "skip_empty": !opt.IncludeEmpty,
	}
	var x Extraction
	return &x, c.call(ctx, "/v1/clusters/"+strconv.Itoa(id)+"/extract", nil, body, &x)
}

// ExtractAll goes through the whole cluster from opt.Offset, part after part
// (following NextOffset), and calls fn for every row. It returns the last
// part without its rows (Regex, PointsLeft, ...). If the day's extraction
// points run out midway, the error is extract_quota_exceeded and the
// returned part's NextOffset is where to resume.
func (c *Client) ExtractAll(ctx context.Context, id int, opt ExtractOptions, fn func(ExtractRow) error) (*Extraction, error) {
	for {
		x, err := c.Extract(ctx, id, opt)
		if err != nil {
			return nil, err
		}
		for _, r := range x.Rows {
			if err := fn(r); err != nil {
				return nil, err
			}
		}
		x.Rows = nil
		if x.NextOffset == nil {
			return x, nil
		}
		if x.Stopped == "extract_quota_exceeded" {
			return x, &Error{Status: 429, Code: "extract_quota_exceeded",
				Message: fmt.Sprintf("the day's extraction points ran out; resume with offset=%d", *x.NextOffset)}
		}
		opt.Offset = *x.NextOffset
	}
}

// RenameCluster gives a cluster a new name.
func (c *Client) RenameCluster(ctx context.Context, id int, name string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.call(ctx, "/v1/clusters/"+strconv.Itoa(id)+"/rename", nil, map[string]any{"name": name}, &cl)
}

// DeleteCluster deletes a cluster for good.
func (c *Client) DeleteCluster(ctx context.Context, id int) (*Deleted, error) {
	var d Deleted
	return &d, c.call(ctx, "/v1/clusters/"+strconv.Itoa(id)+"/delete", nil, map[string]any{}, &d)
}
