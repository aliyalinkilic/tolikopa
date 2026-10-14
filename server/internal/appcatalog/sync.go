package appcatalog

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sync mirrors the embedded catalog into the remediation_item table and marks
// vanished ids deprecated. Call it at startup, right after db.Migrate.
//
// WHY NOT SEED THIS IN A MIGRATION: bloatware catalogs churn with every Windows
// feature update, migrations are append-only forever, and `ON CONFLICT DO
// NOTHING` cannot express retirement — a removed item would linger in the panel
// with no way to retire it short of writing another migration. The table is a UI
// mirror and an audit trail, so it must simply follow the binary.
//
// Retirement is soft (deprecated = true), never a DELETE: remediation_attempt and
// machine_remediation_state rows reference these ids as free text and must stay
// readable after an item leaves the vocabulary.
func Sync(ctx context.Context, pool *pgxpool.Pool) error {
	list := All()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// sort_order preserves catalog.json's hand-curated ordering (games together,
	// OEM junk together, …) so the panel does not have to invent one.
	kinds := make([]string, 0, len(list))
	ids := make([]string, 0, len(list))
	for i, it := range list {
		kinds = append(kinds, it.Kind)
		ids = append(ids, it.ItemID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO remediation_item
				(kind, item_id, family, display_name, category, risk_tier, scope,
				 safe_default, process_names, notes, sort_order, deprecated, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false,now())
			ON CONFLICT (kind, item_id) DO UPDATE SET
				family=EXCLUDED.family, display_name=EXCLUDED.display_name,
				category=EXCLUDED.category, risk_tier=EXCLUDED.risk_tier,
				scope=EXCLUDED.scope, safe_default=EXCLUDED.safe_default,
				process_names=EXCLUDED.process_names, notes=EXCLUDED.notes,
				sort_order=EXCLUDED.sort_order, deprecated=false, updated_at=now()`,
			it.Kind, it.ItemID, it.Family, it.DisplayName, nullStr(it.Category),
			nullStr(it.RiskTier), nullStr(it.Scope), it.SafeDefault,
			nonNilStrings(it.ProcessNames), nullStr(it.Notes), i); err != nil {
			return err
		}
	}

	// Anything the binary no longer carries is retired, not deleted.
	tag, err := tx.Exec(ctx, `
		UPDATE remediation_item ri SET deprecated=true, updated_at=now()
		WHERE ri.deprecated = false
		  AND NOT EXISTS (
			SELECT 1 FROM unnest($1::text[], $2::text[]) AS t(kind, item_id)
			WHERE t.kind = ri.kind AND t.item_id = ri.item_id)`, kinds, ids)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Printf("appcatalog: %d catalog item(s) retired (deprecated)", n)
	}
	log.Printf("appcatalog: synced %d items, vocabulary version %s", len(list), Version())
	return nil
}

// NOTE for callers: the policy revision lives in remediation_policy and a catalog
// sync must NEVER bump it, or every agent would re-fetch the policy after every
// server restart.

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nonNilStrings keeps process_names NOT NULL (defaults to '{}') so callers never
// have to COALESCE it when scanning.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
