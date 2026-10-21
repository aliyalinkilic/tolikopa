package ingest

import (
	"context"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// catRule is one classification rule loaded from process_category.
type catRule struct {
	id         int16
	category   string
	matchType  string // exact | prefix | glob
	matchValue string
	priority   int
}

// Categories is an in-memory cache of the process_category table used to resolve
// a process name to a category_id at ingest time. Resolution priority is
// exact -> prefix -> glob, and within a match type the lowest priority wins.
// It is safe for concurrent use and can be Refresh()ed after admin edits.
type Categories struct {
	pool *pgxpool.Pool

	mu       sync.RWMutex
	exact    map[string]catRule // normalized value -> best rule
	prefixes []catRule          // sorted: priority asc, value length desc
	globs    []catRule          // sorted: priority asc, value length desc
	idToName map[int16]string
}

// NewCategories creates the cache and performs an initial load.
func NewCategories(ctx context.Context, pool *pgxpool.Pool) (*Categories, error) {
	c := &Categories{pool: pool}
	if err := c.Refresh(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Refresh reloads the classification rules from the database.
func (c *Categories) Refresh(ctx context.Context) error {
	rows, err := c.pool.Query(ctx,
		`SELECT id, category, match_type, match_value, priority
		 FROM process_category WHERE enabled = true`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var rules []catRule
	for rows.Next() {
		var r catRule
		if err := rows.Scan(&r.id, &r.category, &r.matchType, &r.matchValue, &r.priority); err != nil {
			return err
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.load(rules)
	return nil
}

// load builds the lookup structures from a rule set and swaps them in.
func (c *Categories) load(rules []catRule) {
	exact := make(map[string]catRule)
	var prefixes, globs []catRule
	idToName := make(map[int16]string)

	for _, r := range rules {
		r.matchValue = strings.ToLower(strings.TrimSpace(r.matchValue))
		idToName[r.id] = r.category
		switch r.matchType {
		case "exact":
			if cur, ok := exact[r.matchValue]; !ok || r.priority < cur.priority {
				exact[r.matchValue] = r
			}
		case "prefix":
			prefixes = append(prefixes, r)
		case "glob":
			globs = append(globs, r)
		}
	}

	byPriority := func(list []catRule) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].priority != list[j].priority {
				return list[i].priority < list[j].priority
			}
			return len(list[i].matchValue) > len(list[j].matchValue)
		})
	}
	byPriority(prefixes)
	byPriority(globs)

	c.mu.Lock()
	c.exact = exact
	c.prefixes = prefixes
	c.globs = globs
	c.idToName = idToName
	c.mu.Unlock()
}

// Resolve maps a raw process name to (category_id, category, ok). ok is false
// when no rule matches (the process stays unclassified with a NULL category_id).
func (c *Categories) Resolve(name string) (int16, string, bool) {
	norm := NormalizeName(name)
	if norm == "" {
		return 0, "", false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1) exact
	if r, ok := c.exact[norm]; ok {
		return r.id, r.category, true
	}
	// 2) prefix (already sorted best-first)
	for _, r := range c.prefixes {
		if strings.HasPrefix(norm, r.matchValue) {
			return r.id, r.category, true
		}
	}
	// 3) glob (path.Match semantics)
	for _, r := range c.globs {
		if ok, err := path.Match(r.matchValue, norm); err == nil && ok {
			return r.id, r.category, true
		}
	}
	return 0, "", false
}

// Match is one enabled rule that matches a process name, as reported by Explain.
type Match struct {
	ID         int16
	Category   string
	MatchType  string
	MatchValue string
	Priority   int
}

// Explain normalises name and returns every enabled rule that matches it, in
// resolution order: the first element is the rule Resolve would pick, the rest
// are shadowed by it. An empty slice means the name stays unclassified.
func (c *Categories) Explain(name string) (string, []Match) {
	norm := NormalizeName(name)
	out := []Match{}
	if norm == "" {
		return norm, out
	}
	add := func(r catRule) {
		out = append(out, Match{ID: r.id, Category: r.category, MatchType: r.matchType, MatchValue: r.matchValue, Priority: r.priority})
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.exact[norm]; ok {
		add(r)
	}
	for _, r := range c.prefixes {
		if strings.HasPrefix(norm, r.matchValue) {
			add(r)
		}
	}
	for _, r := range c.globs {
		if ok, err := path.Match(r.matchValue, norm); err == nil && ok {
			add(r)
		}
	}
	return norm, out
}

// Name returns the category name for a category_id, or "" if unknown.
func (c *Categories) Name(id int16) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.idToName[id]
}

// NormalizeName lowercases, takes the basename and strips a trailing ".exe" so
// the classification map can key on the canonical process name.
//
// Linux kernel threads are named "<thread>/<cpu>[-<queue>]" ("kworker/0:1-events",
// "ksoftirqd/3", "irq/31-vmw_vmci"). Taking the part after the slash turned each
// into a per-CPU fragment ("0:1-events", "3") that no rule could classify and
// that multiplied the distinct names, so a name with a forward slash that is not
// an absolute path keeps only the part before the first slash ("kworker").
// Anything path-like (a backslash, or a leading "/") is still reduced to its
// basename: a path is never stored.
func NormalizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(s, "/") || strings.Contains(s, `\`):
		// Basename for both path separator styles.
		if idx := strings.LastIndexAny(s, `/\`); idx >= 0 {
			s = s[idx+1:]
		}
	case strings.Contains(s, "/"):
		s = s[:strings.Index(s, "/")]
	}
	s = strings.TrimSuffix(s, ".exe")
	return s
}
