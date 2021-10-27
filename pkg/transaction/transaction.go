package transaction

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AddForUpdateQueryOption(db *gorm.DB) *gorm.DB {
	if db.Name() != "sqlite3" {
		// return a new object and not overwrite pointer value because GORM have a pointer to parent
		return db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return db
}
