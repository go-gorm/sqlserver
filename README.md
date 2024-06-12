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
