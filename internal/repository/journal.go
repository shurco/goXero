package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

// JournalRepository exposes the posted General Ledger. This is what Xero's
// `/Journals` endpoint returns: a read-only, dated view of double-entry rows.
type JournalRepository struct {
	pool *pgxpool.Pool
}

type JournalFilter struct {
	From *time.Time
	To   *time.Time
}

func (r *JournalRepository) List(ctx context.Context, orgID uuid.UUID, f JournalFilter, p models.Pagination) ([]models.Journal, int, error) {
	args := []any{orgID}
	where := " WHERE organisation_id=$1"
	idx := 1
	if f.From != nil {
		idx++
		args = append(args, *f.From)
		where += " AND journal_date >= $" + itoa(idx)
	}
	if f.To != nil {
		idx++
		args = append(args, *f.To)
		where += " AND journal_date <= $" + itoa(idx)
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM gl_journals"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := `SELECT journal_id, journal_number, journal_date, created_date_utc,
	             COALESCE(reference,''), source_id, source_type
	        FROM gl_journals` + where +
		" ORDER BY journal_number DESC" +
		" LIMIT $" + itoa(len(args)-1) + " OFFSET $" + itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.Journal
	ids := []uuid.UUID{}
	for rows.Next() {
		var j models.Journal
		if err := rows.Scan(
			&j.JournalID, &j.JournalNumber, &j.JournalDate, &j.CreatedDateUTC,
			&j.Reference, &j.SourceID, &j.SourceType,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, j)
		ids = append(ids, j.JournalID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(ids) > 0 {
		lines, err := r.loadLines(ctx, ids)
		if err != nil {
			return nil, 0, err
		}
		for i := range out {
			out[i].JournalLines = lines[out[i].JournalID]
		}
	}
	return out, total, nil
}

func (r *JournalRepository) loadLines(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]models.JournalLine, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT l.journal_id, l.line_id, l.account_id, COALESCE(a.code,''), COALESCE(a.name,''), COALESCE(a.type,''),
		        COALESCE(l.description,''), COALESCE(l.tax_type,''),
		        l.tax_amount, l.net_amount, l.gross_amount
		   FROM gl_journal_lines l
		   JOIN accounts a ON a.account_id = l.account_id
		  WHERE l.journal_id = ANY($1)
		  ORDER BY l.line_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]models.JournalLine)
	for rows.Next() {
		var jid uuid.UUID
		var l models.JournalLine
		if err := rows.Scan(&jid, &l.JournalLineID, &l.AccountID, &l.AccountCode, &l.AccountName, &l.AccountType,
			&l.Description, &l.TaxType, &l.TaxAmount, &l.NetAmount, &l.GrossAmount); err != nil {
			return nil, err
		}
		out[jid] = append(out[jid], l)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
