package sqlserver

import (
	"github.com/microsoft/go-mssqldb"

	"gorm.io/gorm"
)

// The error codes to map mssql errors to gorm errors, here is a reference about error codes for mssql https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/database-engine-events-and-errors?view=sql-server-ver16
var errCodes = map[string]int32{
	"uniqueConstraint": 2627,
}

type ErrMessage struct {
	Number  int32  `json:"Number"`
	Message string `json:"Message"`
}

// Translate it will translate the error to native gorm errors.
func (dialector Dialector) Translate(err error) error {
	if mssqlErr, ok := err.(mssql.Error); ok {
		if mssqlErr.Number == errCodes["uniqueConstraint"] {
			return gorm.ErrDuplicatedKey
		}
	}

	return err
}
