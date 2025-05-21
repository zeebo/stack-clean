package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/zeebo/errs"
)

func main() {
	printErrors := flag.Bool("e", false, "print errors")
	filterCount := flag.Int("c", 0, "remove stacks with count less than this")
	flag.Parse()

	var lines []string
	var stacks []parsedStack
	var errors int

	addLines := func() {
		if ps, err := parseStack(lines); err == nil {
			stacks = append(stacks, ps)
		} else {
			errors++
		}
		lines = lines[:0]
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			addLines()
			continue
		}
		lines = append(lines, line)
	}
	addLines()

	sort.Slice(stacks, func(i, j int) bool { return stacks[i].key < stacks[j].key })

	group(stacks, func(n int, ps []parsedStack) {
		if n < *filterCount {
			return
		}
		minWait, maxWait := minMax(ps)
		fmt.Printf("count:%d waiting:%d-%d status:%s\n", n, minWait, maxWait, strings.Join(sortedStatuses(ps), ", "))
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, f := range ps[0].frames {
			fmt.Fprintf(tw, "%s:%d\t%s\n", filepath.Base(f.path), f.line, f.fn)
		}
		tw.Flush()
		fmt.Println()
	})

	if *printErrors {
		fmt.Printf("errors:%d\n", errors)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minMax(ps []parsedStack) (minWait, maxWait int) {
	minWait, maxWait = ps[0].waiting, ps[0].waiting
	for _, p := range ps[1:] {
		minWait = min(minWait, p.waiting)
		maxWait = max(maxWait, p.waiting)
	}
	return minWait, maxWait
}

func sortedStatuses(ps []parsedStack) []string {
	ss := make(map[string]struct{})
	for _, p := range ps {
		ss[p.status] = struct{}{}
	}
	statuses := make([]string, 0, len(ss))
	for s := range ss {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	return statuses
}

func group(ps []parsedStack, cb func(n int, ps []parsedStack)) {
	if len(ps) == 0 {
		return
	}

	prev := 0
	count := 1
	for i := 1; i < len(ps); i++ {
		if ps[i].key == ps[prev].key {
			count++
			continue
		}

		cb(count, ps[prev:i])
		prev = i
		count = 1
	}
	cb(count, ps[prev:])
}

type frame struct {
	fn     string
	args   string
	path   string
	line   int
	offset uintptr
}

type parsedStack struct {
	goroutine int
	status    string
	waiting   int

	frames  []frame
	created frame

	key string
}

var (
	goroutineMatcher = regexp.MustCompile(`^goroutine (\d+) \[([^,]+)(, (\d+) minutes)?\]:$`)
	createdMatcher   = regexp.MustCompile(`^created by (.+) in goroutine (\d+)$`)
	locationMatcher  = regexp.MustCompile(`^(.+):(\d+)( \+(0x[0-9a-f]+))?$`)
	functionMatcher  = regexp.MustCompile(`^(.+)\((.*)\)$`)
)

func parseStack(lines []string) (ps parsedStack, err error) {
	if len(lines) < 3 {
		return ps, errors.New("not enough lines")
	}

	var p parser

	matches := p.regexp(lines[0], goroutineMatcher)
	ps.goroutine = int(p.digits(matches[1]))
	ps.status = matches[2]
	ps.waiting = int(p.digits(matches[4]))

	if lines[len(lines)-2] == "main.main()" {
		ps.created.fn = "-"
	} else {
		matches = p.regexp(lines[len(lines)-2], createdMatcher)
		ps.created.fn = matches[1]
	}

	matches = p.regexp(lines[len(lines)-1], locationMatcher)
	ps.created.path = matches[1]
	ps.created.line = int(p.digits(matches[2]))
	ps.created.offset = uintptr(p.digits(matches[4]))

	for i := 1; i < len(lines)-2; i += 2 {
		var f frame

		matches = p.regexp(lines[i], functionMatcher)
		f.fn = matches[1]
		f.args = matches[2]

		matches = p.regexp(lines[i+1], locationMatcher)
		f.path = matches[1]
		f.line = int(p.digits(matches[2]))
		f.offset = uintptr(p.digits(matches[4]))

		ps.frames = append(ps.frames, f)
	}

	var b strings.Builder
	b.WriteString(ps.status)
	b.WriteByte('\n')
	for _, f := range ps.frames {
		b.WriteString(f.fn)
		b.WriteByte('\n')
	}
	ps.key = b.String()

	return ps, p.err
}

type parser struct {
	err error
}

func (p *parser) digits(s string) (n uint64) {
	if p.err != nil {
		return 0
	} else if s == "" {
		return 0
	}
	n, p.err = strconv.ParseUint(s, 0, 64)
	return n
}

func (p *parser) regexp(s string, re *regexp.Regexp) (matches []string) {
	if p.err != nil {
		return make([]string, re.NumSubexp()+1)
	}
	matches = re.FindStringSubmatch(s)
	if matches == nil {
		p.err = errs.New("no match: %q (%v)", s, re)
		return make([]string, re.NumSubexp()+1)
	}
	return matches
}
