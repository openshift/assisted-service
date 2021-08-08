package sqllite

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

//go:generate mockgen -source=query.go -package=sqllite -destination=mock_query.go
type Query interface {
	GetOperatorVersions(bundleName string) ([]string, error)
}

type connection struct {
	dbFile string
}

func NewQuery(dbFile string) Query {
	return &connection{dbFile}
}

// bundleQuery run a command to get available versions in the operatorbundle table, where
// the name starts by bundleName query
func (q *connection) GetOperatorVersions(bundleName string) ([]string, error) {
	return q.query(fmt.Sprintf("select version from operatorbundle where name like \"%s%%\"", bundleName))
}

// query runs the query command to the sqlite3 backend and scanning all the values it observes
// the select command must be written to get only single value
func (q *connection) query(query string) ([]string, error) {
	db, err := sql.Open("sqlite3", q.dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var vals []string
	var val string
	for rows.Next() {
		err = rows.Scan(&val)
		if err != nil {
			log.Fatal(err)
		}
		vals = append(vals, val)
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return vals, nil
}
