# GORM SQL Server Driver

## USAGE

```go
import (
  "gorm.io/driver/sqlserver"
  "gorm.io/gorm"
)

// github.com/microsoft/go-mssqldb
dsn := "sqlserver://gorm:LoremIpsum86@localhost:9930?database=gorm"
db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
```

## Pagination compatibility

This driver detects the SQL Server version at connection time.

- SQL Server 2012 and newer use the native `OFFSET ... FETCH NEXT` pagination syntax.
- SQL Server 2008 / 2008 R2 and earlier automatically fallback to a legacy pagination strategy built from
  `TOP()` and `ROW_NUMBER()` over the generated query.

This keeps the GORM `LIMIT`/`OFFSET` semantics working across mixed SQL Server installations
without changing application code.

## Azure AD Auth

```go
import (
  "github.com/microsoft/go-mssqldb/azuread"
  "gorm.io/driver/sqlserver"
  "gorm.io/gorm"
)

// github.com/microsoft/go-mssqldb
dsn := "sqlserver://gorm:LoremIpsum86@localhost:9930?database=gorm"
dialector := &sqlserver.Dialector{Config: &sqlserver.Config{DSN: dsn, DriverName: azuread.DriverName}}
db, err := gorm.Open(dialector, &gorm.Config{})
```

Checkout [https://gorm.io](https://gorm.io) for details.
