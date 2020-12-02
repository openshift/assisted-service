## Migrations

### Why?

There are some database changes that GORM auto migration [will not handle for us](https://v1.gorm.io/docs/migration.html#Auto-Migration).
These need to be dealt with separately.

### How?

To do this we are using a migration module called [gormigrate](https://pkg.go.dev/gopkg.in/gormigrate.v1).
Migrations are run at startup right after the automigration is run.

### Adding a new migration

- Each migration should have a separate file in `/internal/migrations` prefixed with a timestamp which will also be the migration ID
- Each migration should have a single function named to describe the change which returns a `*gormigrate.Migration`
    - Both migrate (up) and rollback (down) should be implemented if possible
- Every migration should have a corresponding test (especially for migrations which are changing data)
- A new migration scaffold can be created using `MIGRATION_NAME=someMigrationNameHere make generate-migration`
    - This will give the migration files a proper timestamp as well as (empty) tests

To be run, every migration function should be added to the list in `migrations.all()`
