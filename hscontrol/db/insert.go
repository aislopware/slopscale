package db

import (
	"fmt"
)

// insertReturningID runs an INSERT whose RETURNING clause aliases the new
// row's id as "id_row.id" and returns that id. A unique violation, which
// for these tables is a taken name, comes back as nameTaken; any other
// failure is wrapped with what.
func insertReturningID(q Querier, stmt statement, what string, nameTaken error) (uint64, error) {
	var inserted idRow

	err := q.executor().query(stmt, &inserted)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, nameTaken
		}

		return 0, fmt.Errorf("creating %s: %w", what, err)
	}

	return inserted.ID, nil
}
