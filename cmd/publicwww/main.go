// Command publicwww searches PublicWWW from the command line.
//
//	publicwww account
//	publicwww search '"angular.min.js"' [per_page]
//	publicwww export '"angular.min.js"' > sites.txt
//	publicwww cluster-create --query '"googletagmanager.com/gtm.js"'
//	publicwww cluster-create --query 'like:"online course" site:de' --limit 100
//	publicwww extract 12 --presets email,phone > contacts.csv
//	publicwww signatures 12
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/publicwww-com/publicwww-api-go"
)

const usage = `usage: publicwww account | search QUERY [PER_PAGE] | export QUERY
       | clusters | cluster ID | cluster-create (--query Q [--limit N] | --domains-file F) [--name N]
       | cluster-combine and|or|diff ID ID... [--name N] | presets
       | extract ID [--presets a,b] [--regex R]... [--offset N] [--delimiter C] | signatures ID
       | cluster-rename ID NAME | cluster-delete ID`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseArgs splits positional arguments from --options; --regex may repeat.
func parseArgs(args []string) (pos []string, opt map[string]string, regex []string) {
	opt = map[string]string{}
	for i := 0; i < len(args); i++ {
		name, ok := strings.CutPrefix(args[i], "--")
		if !ok {
			pos = append(pos, args[i])
			continue
		}
		v := ""
		if i+1 < len(args) {
			i++
			v = args[i]
		}
		if name == "regex" {
			regex = append(regex, v)
		} else {
			opt[name] = v
		}
	}
	return pos, opt, regex
}

// ids turns cluster ids from the command line into numbers.
func ids(args []string) ([]int, error) {
	out := make([]int, len(args))
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return nil, fmt.Errorf("not a cluster id: %q", a)
		}
		out[i] = n
	}
	return out, nil
}

func clusterLine(c *publicwww.Cluster) {
	fmt.Printf("%d\t%d\t%s\t%s\n", c.ID, c.Size, c.Created, c.Name)
}

func run(args []string) error {
	need := map[string]int{"account": 0, "search": 1, "export": 1, "clusters": 0, "cluster": 1, "cluster-create": 0,
		"cluster-combine": 3, "presets": 0, "extract": 1, "signatures": 1, "cluster-rename": 2, "cluster-delete": 1}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	pos, opt, regex := parseArgs(args[1:])
	if n, ok := need[args[0]]; !ok || len(pos) < n ||
		(args[0] == "cluster-create" && opt["query"] == "" && opt["domains-file"] == "") {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	pw, err := publicwww.New("")
	if err != nil {
		return err
	}
	ctx := context.Background()

	switch args[0] {
	case "account":
		a, err := pw.Account(ctx)
		if err != nil {
			return err
		}
		out, _ := json.MarshalIndent(a, "", "  ")
		fmt.Println(string(out))
	case "search":
		perPage := 20
		if len(args) > 2 {
			perPage, _ = strconv.Atoi(args[2])
		}
		p, err := pw.Search(ctx, args[1], 1, perPage)
		if err != nil {
			return err
		}
		note := ""
		if p.Truncated {
			note = " (cut short by the plan)"
		}
		more := ""
		if p.TotalCapped {
			more = "more than "
		}
		fmt.Printf("%s%d sites match, page %d of %d%s\n", more, p.Total, p.Page, p.TotalPages, note)
		if p.QueryNote != "" {
			fmt.Fprintln(os.Stderr, "note: "+p.QueryNote)
		}
		for _, r := range p.Results {
			rank := ""
			if r.Rank != nil {
				rank = strconv.Itoa(*r.Rank)
			}
			fmt.Printf("%s\t%s\t%s\n", rank, r.Domain, r.URL)
		}
	case "export":
		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()
		meta, err := pw.IterAll(ctx, args[1], 0, func(r publicwww.Result) error {
			_, err := fmt.Fprintln(w, r.URL)
			return err
		})
		if err != nil {
			return err
		}
		if meta.Truncated {
			fmt.Fprintf(os.Stderr, "note: the plan returned %d of %d\n", meta.Returned, meta.Total)
		}
	case "clusters":
		l, err := pw.Clusters(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%d clusters of at most %d\n", l.Count, l.Max)
		for i := range l.Clusters {
			clusterLine(&l.Clusters[i])
		}
	case "cluster":
		id, err := ids(pos[:1])
		if err != nil {
			return err
		}
		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()
		return pw.ClusterDomains(ctx, id[0], func(d string) error {
			_, err := fmt.Fprintln(w, d)
			return err
		})
	case "cluster-create":
		var c *publicwww.Cluster
		if opt["query"] != "" {
			limit := 0
			if opt["limit"] != "" {
				if limit, err = strconv.Atoi(opt["limit"]); err != nil {
					return fmt.Errorf("not a limit: %q", opt["limit"])
				}
			}
			c, err = pw.CreateClusterLimit(ctx, opt["query"], opt["name"], limit)
		} else {
			var data []byte
			if f := opt["domains-file"]; f == "-" {
				data, err = io.ReadAll(os.Stdin)
			} else {
				data, err = os.ReadFile(f)
			}
			if err != nil {
				return err
			}
			var domains []string
			for _, s := range strings.Split(string(data), "\n") {
				if s = strings.TrimSpace(s); s != "" {
					domains = append(domains, s)
				}
			}
			c, err = pw.CreateClusterFromDomains(ctx, domains, opt["name"])
		}
		if err != nil {
			return err
		}
		clusterLine(c)
		if c.QueryNote != "" {
			fmt.Fprintln(os.Stderr, "note: "+c.QueryNote)
		}
	case "cluster-combine":
		from, err := ids(pos[1:])
		if err != nil {
			return err
		}
		c, err := pw.CombineClusters(ctx, pos[0], from, opt["name"])
		if err != nil {
			return err
		}
		clusterLine(c)
	case "presets":
		ps, err := pw.ClusterPresets(ctx)
		if err != nil {
			return err
		}
		for _, p := range ps {
			fmt.Printf("%s\t%s\n", p.Name, p.About)
		}
	case "extract":
		id, err := ids(pos[:1])
		if err != nil {
			return err
		}
		var presets []string
		for _, p := range strings.Split(opt["presets"], ",") {
			if p = strings.TrimSpace(p); p != "" {
				presets = append(presets, p)
			}
		}
		offset := 0
		if opt["offset"] != "" {
			if offset, err = strconv.Atoi(opt["offset"]); err != nil {
				return fmt.Errorf("not an offset: %q", opt["offset"])
			}
		}
		for {
			x, err := pw.ExtractCSV(ctx, id[0], publicwww.ExtractOptions{Presets: presets, Regex: regex, Offset: offset},
				opt["delimiter"], offset == 0)
			if err != nil {
				return err
			}
			os.Stdout.Write(x.CSV)
			if x.NextOffset == nil {
				fmt.Fprintf(os.Stderr, "%d extraction points left\n", x.PointsLeft)
				break
			}
			offset = *x.NextOffset
			if x.PointsLeft <= 0 {
				return fmt.Errorf("the day's extraction points ran out; resume with --offset %d", offset)
			}
		}
	case "signatures":
		id, err := ids(pos[:1])
		if err != nil {
			return err
		}
		res, err := pw.Signatures(ctx, id[0])
		if err != nil {
			return err
		}
		for _, s := range res.Signatures {
			websites := ""
			if s.Websites != nil {
				websites = strconv.Itoa(*s.Websites)
			} else if s.WebsitesMax != nil {
				websites = "<" + strconv.Itoa(*s.WebsitesMax)
			}
			fmt.Printf("%s\t%d\t%s\t%s\n", s.Query, s.InCluster, websites, s.Technology)
		}
		if res.Note != "" {
			fmt.Fprintln(os.Stderr, "note: "+res.Note)
		}
	case "cluster-rename":
		id, err := ids(pos[:1])
		if err != nil {
			return err
		}
		c, err := pw.RenameCluster(ctx, id[0], pos[1])
		if err != nil {
			return err
		}
		clusterLine(c)
	case "cluster-delete":
		id, err := ids(pos[:1])
		if err != nil {
			return err
		}
		d, err := pw.DeleteCluster(ctx, id[0])
		if err != nil {
			return err
		}
		fmt.Printf("deleted %d\t%s\n", d.Deleted, d.Name)
	}
	return nil
}
