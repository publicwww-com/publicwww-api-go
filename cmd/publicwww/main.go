// Command publicwww searches PublicWWW from the command line.
//
//	publicwww account
//	publicwww search '"angular.min.js"' [per_page]
//	publicwww export '"angular.min.js"' > sites.txt
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/publicwww-com/publicwww-api-go"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || (args[0] != "account" && len(args) < 2) {
		fmt.Fprintln(os.Stderr, "usage: publicwww account | search QUERY [PER_PAGE] | export QUERY")
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
		fmt.Printf("%d sites match, page %d of %d%s\n", p.Total, p.Page, p.TotalPages, note)
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
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
