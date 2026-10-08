// Package publicwww is a small client for the PublicWWW API -
// https://publicwww.com/docs/api/ - standard library only.
//
//	pw, err := publicwww.New("")                     // key from $PUBLICWWW_KEY
//	page, err := pw.Search(ctx, `"angular.min.js"`, 1, 20)
//	err = pw.IterAll(ctx, `"angular.min.js"`, 0, func(r publicwww.Result) error {
//		fmt.Println(r.Domain)
//		return nil
//	})
package publicwww

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const userAgent = "publicwww-api-go/1.0"

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

// get performs a GET and returns a 200 response. It waits out the rate limit;
// any other error is returned. The caller closes the body.
func (c *Client) get(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	u := c.API + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("User-Agent", userAgent)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == 200 {
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

// Account returns the plan, quota and limits. It spends nothing.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	res, err := c.get(ctx, "/v1/account", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var a Account
	return &a, json.NewDecoder(res.Body).Decode(&a)
}

// Search returns one page of results.
func (c *Client) Search(ctx context.Context, query string, page, perPage int) (*Page, error) {
	res, err := c.get(ctx, "/v1/search", url.Values{
		"query": {query}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
	})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var p Page
	return &p, json.NewDecoder(res.Body).Decode(&p)
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
	})
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
