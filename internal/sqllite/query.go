package sqllite

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=query.go -package=sqllite -destination=mock_query.go
type OperatorVersionReader interface {
	GetOperatorVersionsFromDB(dbFile, bundleName string) ([]string, error)
}

type sqlLiteOperatorVersionReader struct {
	log logrus.FieldLogger
}

func NewOperatorVersionReader(log logrus.FieldLogger) OperatorVersionReader {
	return &sqlLiteOperatorVersionReader{log}
}

// GetOperatorVersionsFromDB runs a command to get available versions in the operatorbundle table in the dbFile, where the name starts by bundleName query
func (sqlLiteOperatorVersionReader *sqlLiteOperatorVersionReader) GetOperatorVersionsFromDB(dbFile, bundleName string) ([]string, error) {
	return sqlLiteOperatorVersionReader.query(dbFile, fmt.Sprintf("select version from operatorbundle where name like \"%s%%\"", bundleName))
}

// query runs the query command to the sqlite3 backend and scanning all the values it observes the select command must be written to get only single value
func (sqlLiteOperatorVersionReader *sqlLiteOperatorVersionReader) query(dbFile, query string) ([]string, error) {
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		sqlLiteOperatorVersionReader.log.Error(err)
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		sqlLiteOperatorVersionReader.log.Error(err)
		return nil, err
	}
	defer rows.Close()

	var vals []string
	var val string
	for rows.Next() {
		err = rows.Scan(&val)
		if err != nil {
			sqlLiteOperatorVersionReader.log.Error(err)
			return nil, err
		}
		vals = append(vals, val)
	}
	err = rows.Err()
	if err != nil {
		sqlLiteOperatorVersionReader.log.Error(err)
		return nil, err
	}

	return vals, nil
}
