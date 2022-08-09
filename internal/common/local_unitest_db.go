package common

import "strconv"

// LocalDBContext is a DBContext that doesn't run a database but instead simply
// assumes it's already available at localhost:5432
type LocalDBContext struct{}

func (c *LocalDBContext) RunDatabase() error {
	return nil
}

func (c *LocalDBContext) TeardownDatabase() {}

func (c *LocalDBContext) GetDatabaseHostPort() (string, string) {
	return "127.0.0.1", strconv.Itoa(databaseDefaultPort)
}
