// Package store persists reconciliation state in PostgreSQL. Schema-level
// constraints back invariants 2 and 4; see migrations/0001_init.sql.
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"reconpilot/internal/classification"
	"reconpilot/internal/domain"
	"reconpilot/internal/matching"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) CreateBatch(ctx context.Context, source domain.SourceType, fileRef string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO source_batch (source_type, file_ref) VALUES ($1,$2) RETURNING id`,
		string(source), fileRef).Scan(&id)
	return id, err
}

func (s *Store) UpsertTransactions(ctx context.Context, batchID int64, txs []domain.Transaction) (int, error) {
	inserted := 0
	for _, tx := range txs {
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO transaction (batch_id, source_type, external_ref, counterparty_ref,
				amount_kurus, currency, occurred_at, direction, dedup_key, status,
				gross_kurus, commission_kurus)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (dedup_key) DO NOTHING`,
			batchID, string(tx.Source), tx.ExternalRef, tx.CounterpartyRef,
			tx.AmountKurus, tx.Currency, tx.OccurredAt, string(tx.Direction),
			tx.DedupKey, string(tx.Status), tx.GrossKurus, tx.CommissionKurus)
		if err != nil {
			return inserted, err
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

func (s *Store) InsertRejectedRow(ctx context.Context, batchID int64, rowNo int, reason, raw string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO rejected_row (batch_id, row_no, reason, raw_line) VALUES ($1,$2,$3,$4)`,
		batchID, rowNo, reason, raw)
	return err
}

func (s *Store) LoadTransactions(ctx context.Context) ([]domain.Transaction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, batch_id, source_type, external_ref, counterparty_ref, amount_kurus,
		       currency, occurred_at, direction, dedup_key, status, gross_kurus, commission_kurus
		FROM transaction ORDER BY occurred_at, external_ref, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		var src, dir, status string
		if err := rows.Scan(&t.ID, &t.BatchID, &src, &t.ExternalRef, &t.CounterpartyRef,
			&t.AmountKurus, &t.Currency, &t.OccurredAt, &dir, &t.DedupKey, &status,
			&t.GrossKurus, &t.CommissionKurus); err != nil {
			return nil, err
		}
		t.Source, t.Direction, t.Status = domain.SourceType(src), domain.Direction(dir), domain.TxStatus(status)
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveResult persists an engine run atomically. The schema's
// UNIQUE(transaction_id) turns any double-membership bug into a hard DB
// error here — defence in depth for invariant 2.
func (s *Store) SaveResult(ctx context.Context, matches []matching.Match, discs []classification.Discrepancy) error {
	return pgx.BeginFunc(ctx, s.pool, func(dbtx pgx.Tx) error {
		// Recompute semantics: a run atomically replaces the previous run's
		// results, so `recon run` is safely re-runnable — result-level
		// idempotence mirroring invariant 4's ingest-level idempotence.
		for _, q := range []string{
			`DELETE FROM discrepancy`,
			`DELETE FROM match_member`,
			`DELETE FROM match_group`,
			`UPDATE transaction SET status='unmatched'`,
		} {
			if _, err := dbtx.Exec(ctx, q); err != nil {
				return err
			}
		}
		groupIDs := make([]int64, len(matches))
		for i, m := range matches {
			if err := dbtx.QueryRow(ctx,
				`INSERT INTO match_group (kind, score) VALUES ($1,$2) RETURNING id`,
				m.Kind, m.ScoreBP).Scan(&groupIDs[i]); err != nil {
				return err
			}
			for _, id := range m.TxIDs {
				if _, err := dbtx.Exec(ctx,
					`INSERT INTO match_member (match_group_id, transaction_id) VALUES ($1,$2)`,
					groupIDs[i], id); err != nil {
					return err
				}
				if _, err := dbtx.Exec(ctx,
					`UPDATE transaction SET status='matched' WHERE id=$1`, id); err != nil {
					return err
				}
			}
		}
		for _, d := range discs {
			var gid *int64
			if d.MatchIdx != nil {
				gid = &groupIDs[*d.MatchIdx]
			}
			if _, err := dbtx.Exec(ctx,
				`INSERT INTO discrepancy (transaction_id, match_group_id, type, amount_delta_kurus)
				 VALUES ($1,$2,$3,$4)`,
				d.TxID, gid, d.Type, d.DeltaKurus); err != nil {
				return err
			}
		}
		return nil
	})
}

// insertRawMatch is a low-level helper used by tests and SaveResult.
func (s *Store) insertRawMatch(ctx context.Context, kind string, txIDs []int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(dbtx pgx.Tx) error {
		var gid int64
		if err := dbtx.QueryRow(ctx,
			`INSERT INTO match_group (kind) VALUES ($1) RETURNING id`, kind).Scan(&gid); err != nil {
			return err
		}
		for _, id := range txIDs {
			if _, err := dbtx.Exec(ctx,
				`INSERT INTO match_member (match_group_id, transaction_id) VALUES ($1,$2)`, gid, id); err != nil {
				return err
			}
		}
		return nil
	})
}
