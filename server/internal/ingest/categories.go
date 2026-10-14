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

	exact := make(map[string]catRule)
	var prefixes, globs []catRule
	idToName := make(map[int16]string)

	for rows.Next() {
		var r catRule
		if err := rows.Scan(&r.id, &r.category, &r.matchType, &r.matchValue, &r.priority); err != nil {
			return err
		}
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
	if err := rows.Err(); err != nil {
		return err
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
	return nil
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

// Name returns the category name for a category_id, or "" if unknown.
func (c *Categories) Name(id int16) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.idToName[id]
}

// NormalizeName lowercases, takes the basename and strips a trailing ".exe" so
// the classification map can key on the canonical process name.
func NormalizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	// Basename for both path separator styles.
	if idx := strings.LastIndexAny(s, `/\`); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimSuffix(s, ".exe")
	return s
}
