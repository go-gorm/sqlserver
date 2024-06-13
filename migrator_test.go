package sqlserver_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

var sqlserverDSN = "sqlserver://gorm:LoremIpsum86@localhost:9930?database=gorm"

func init() {
	if dbDSN := os.Getenv("GORM_DSN"); dbDSN != "" {
		sqlserverDSN = dbDSN
	}
}

type Testtable struct {
	Test uint64 `gorm:"index"`
}

type Testtable2 struct {
	Test  uint64 `gorm:"index"`
	Test2 uint64
}

func (*Testtable2) TableName() string { return "testtables" }

type Testtable3 struct {
	Test3 uint64
}

func (*Testtable3) TableName() string { return "testschema1.Testtables" }

type Testtable4 struct {
	Test4 uint64
}

func (*Testtable4) TableName() string { return "testschema2.Testtables" }

type Testtable5 struct {
	Test4 uint64
	Test5 uint64 `gorm:"index"`
}

func (*Testtable5) TableName() string { return "testschema2.Testtables" }

func TestAutomigrateTablesWithoutDefaultSchema(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Error(err)
	}

	if tx := db.Exec("create schema testschema1"); tx.Error != nil {
		t.Error("couldn't create schema testschema1", tx.Error)
	}
	if tx := db.Exec("create schema testschema2"); tx.Error != nil {
		t.Error("couldn't create schema testschema2", tx.Error)
	}

	if err = db.AutoMigrate(&Testtable{}); err != nil {
		t.Error("couldn't create a table at user default schema", err)
	}
	if err = db.AutoMigrate(&Testtable2{}); err != nil {
		t.Error("couldn't update a table at user default schema", err)
	}
	if err = db.AutoMigrate(&Testtable3{}); err != nil {
		t.Error("couldn't create a table at schema testschema1", err)
	}
	if err = db.AutoMigrate(&Testtable4{}); err != nil {
		t.Error("couldn't create a table at schema testschema2", err)
	}
	if err = db.AutoMigrate(&Testtable5{}); err != nil {
		t.Error("couldn't update a table at schema testschema2", err)
	}

	if tx := db.Exec("drop table testtables"); tx.Error != nil {
		t.Error("couldn't drop table testtable at user default schema", tx.Error)
	}

	if tx := db.Exec("drop table testschema1.testtables"); tx.Error != nil {
		t.Error("couldn't drop table testschema1.testtable", tx.Error)
	}

	if tx := db.Exec("drop table testschema2.testtables"); tx.Error != nil {
		t.Error("couldn't drop table testschema2.testtable", tx.Error)
	}

	if tx := db.Exec("drop schema testschema1"); tx.Error != nil {
		t.Error("couldn't drop schema testschema1", tx.Error)
	}

	if tx := db.Exec("drop schema testschema2"); tx.Error != nil {
		t.Error("couldn't drop schema testschema2", tx.Error)
	}

}

type Testtable6 struct {
	ID string `gorm:"index:unique_id,class:UNIQUE,where:id IS NOT NULL"`
}

func (*Testtable6) TableName() string { return "testtable" }

func TestCreateIndex(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Error(err)
	}
	if err = db.AutoMigrate(&Testtable6{}); err != nil {
		t.Error("couldn't create table at user default schema", err)
	}
	if tx := db.Exec("drop table testtable"); tx.Error != nil {
		t.Error("couldn't drop table testtable", tx.Error)
	}
}

type TestTableDefaultValue struct {
	ID        string     `gorm:"column:id;primaryKey"`
	Name      string     `gorm:"column:name"`
	Age       uint       `gorm:"column:age"`
	Birthday  *time.Time `gorm:"column:birthday"`
	CompanyID *int       `gorm:"column:company_id;default:0"`
	ManagerID *uint      `gorm:"column:manager_id;default:0"`
	Active    bool       `gorm:"column:active;default:1"`
}

func (*TestTableDefaultValue) TableName() string { return "test_table_default_value" }

func TestReMigrateTableFieldsWithoutDefaultValue(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Error(err)
	}

	var (
		migrator             = db.Migrator()
		tableModel           = new(TestTableDefaultValue)
		fieldsWithDefault    = []string{"company_id", "manager_id", "active"}
		fieldsWithoutDefault = []string{"id", "name", "age", "birthday"}

		columnsWithDefault    []string
		columnsWithoutDefault []string
	)

	defer func() {
		if err = migrator.DropTable(tableModel); err != nil {
			t.Errorf("couldn't drop table %q, got error: %v", tableModel.TableName(), err)
		}
	}()
	if !migrator.HasTable(tableModel) {
		if err = migrator.AutoMigrate(tableModel); err != nil {
			t.Errorf("couldn't auto migrate table %q, got error: %v", tableModel.TableName(), err)
		}
	}
	// If in the `Migrator.ColumnTypes` method `column.DefaultValueValue.Valid = true`,
	// re-migrate the table will alter all fields without default value except for the primary key.
	if err = db.Debug().Migrator().AutoMigrate(tableModel); err != nil {
		t.Errorf("couldn't re-migrate table %q, got error: %v", tableModel.TableName(), err)
	}

	columnsWithDefault, columnsWithoutDefault, err = testGetMigrateColumns(db, tableModel)
	if !reflect.DeepEqual(columnsWithDefault, fieldsWithDefault) {
		// If in the `Migrator.ColumnTypes` method `column.DefaultValueValue.Valid = true`,
		// fields with default value will include all fields: `[id name age birthday company_id manager_id active]`.
		t.Errorf("expected columns with default value %v, got %v", fieldsWithDefault, columnsWithDefault)
	}
	if !reflect.DeepEqual(columnsWithoutDefault, fieldsWithoutDefault) {
		t.Errorf("expected columns without default value %v, got %v", fieldsWithoutDefault, columnsWithoutDefault)
	}
}

func testGetMigrateColumns(db *gorm.DB, dst interface{}) (columnsWithDefault, columnsWithoutDefault []string, err error) {
	migrator := db.Migrator()
	var columnTypes []gorm.ColumnType
	if columnTypes, err = migrator.ColumnTypes(dst); err != nil {
		return
	}
	for _, columnType := range columnTypes {
		if _, ok := columnType.DefaultValue(); ok {
			columnsWithDefault = append(columnsWithDefault, columnType.Name())
		} else {
			columnsWithoutDefault = append(columnsWithoutDefault, columnType.Name())
		}
	}
	return
}
