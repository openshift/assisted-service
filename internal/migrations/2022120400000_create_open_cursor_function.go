package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const createOpenCursorFunctionID = "20221204000000"

const createOpenCursorFunctionSQL = `
create or replace function open_cursor(ref refcursor, query text) returns void as '
begin
	open ref for execute query;
end;
' language plpgsql
`

const dropOpenCursorFunctionSQL = `
drop function if exists open_cursor(ref refcursor, query text);
`

func createOpenCursorFunction() *gormigrate.Migration {
	migrate := func(tx *gorm.DB) error {
		return tx.Exec(createOpenCursorFunctionSQL).Error
	}
	rollback := func(tx *gorm.DB) error {
		return tx.Exec(dropOpenCursorFunctionSQL).Error
	}
	return &gormigrate.Migration{
		ID:       createOpenCursorFunctionID,
		Migrate:  migrate,
		Rollback: rollback,
	}
}
