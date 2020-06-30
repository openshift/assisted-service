package transaction

import "github.com/jinzhu/gorm"

func AddForUpdateQueryOption(db *gorm.DB) {
	if db.Dialect().GetName() != "sqlite3" {
		*db = *db.Set("gorm:query_option", "FOR UPDATE")
	}
}
