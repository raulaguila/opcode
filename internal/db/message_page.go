package db

import "context"

type MessagePageParams struct {
	SessionID                    string
	BeforeCreatedAt, BeforeRowID int64
	Limit                        int
}

type MessagePageRow struct {
	Message
	RowID int64
}

// ListMessagePage reads presentation history only. Keep large binary payloads
// and opaque provider continuations in the authoritative Get/List APIs.
func (q *Queries) ListMessagePage(ctx context.Context, arg MessagePageParams) ([]MessagePageRow, error) {
	where := "session_id = ?"
	args := []any{arg.SessionID}
	if arg.BeforeRowID > 0 {
		where += " AND (created_at,rowid) < (?,?)"
		args = append(args, arg.BeforeCreatedAt, arg.BeforeRowID)
	}
	args = append(args, arg.Limit)
	query := `SELECT id,session_id,role,
(SELECT json_group_array(json(CASE json_extract(value,'$.type')
WHEN 'binary' THEN json_remove(value,'$.data.Data')
WHEN 'finish' THEN json_remove(value,'$.data.provider_state')
ELSE value END)) FROM json_each(parts)),
model,created_at,updated_at,finished_at,sequence
FROM (SELECT *,rowid AS sequence FROM messages WHERE ` + where + ` ORDER BY created_at DESC,rowid DESC LIMIT ?)
ORDER BY created_at DESC,sequence DESC`
	rows, err := q.query(ctx, nil, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MessagePageRow{}
	for rows.Next() {
		var item MessagePageRow
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Parts, &item.Model, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt, &item.RowID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
