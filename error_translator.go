package sqlserver

import (
	"github.com/microsoft/go-mssqldb"

	"gorm.io/gorm"
)

// The error codes to map mssql errors to gorm errors, here is a reference about error codes for mssql https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/database-engine-events-and-errors?view=sql-server-ver16
var errCodes = map[int32]error{
	2627: gorm.ErrDuplicatedKey,
	2601: gorm.ErrDuplicatedKey,
	547:  gorm.ErrForeignKeyViolated,
}

type ErrMessage struct {
	Number  int32  `json:"Number"`
	Message string `json:"Message"`
}

// Translate it will translate the error to native gorm errors.
func (dialector Dialector) Translate(err error) error {
	if mssqlErr, ok := err.(mssql.Error); ok {
		if translatedErr, found := errCodes[mssqlErr.Number]; found {
			return translatedErr
		}
		return err
	}

	return err
}
