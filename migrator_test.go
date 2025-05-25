package sqlserver_test

import (
	"encoding/json"
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

type TestTableFieldComment struct {
	ID   string `gorm:"column:id;primaryKey;comment:"` // field comment is an empty string
	Name string `gorm:"column:name;comment:姓名"`
	Age  uint   `gorm:"column:age;comment:年龄"`
}

func (*TestTableFieldComment) TableName() string { return "test_table_field_comment" }

type TestTableFieldCommentUpdate struct {
	ID       string     `gorm:"column:id;primaryKey;comment:ID"`
	Name     string     `gorm:"column:name;comment:姓名"`
	Age      uint       `gorm:"column:age;comment:周岁"`
	Birthday *time.Time `gorm:"column:birthday;comment:生日"`
	Quote    string     `gorm:"column:quote;comment:注释中包含'单引号'和特殊符号❤️"`
}

func (*TestTableFieldCommentUpdate) TableName() string { return "test_table_field_comment" }

func TestMigrator_MigrateColumnComment(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}
	dm := db.Debug().Migrator()

	tableModel := new(TestTableFieldComment)
	defer func() {
		if err = dm.DropTable(tableModel); err != nil {
			t.Errorf("couldn't drop table %q, got error: %v", tableModel.TableName(), err)
		}
	}()

	if err = dm.AutoMigrate(tableModel); err != nil {
		t.Fatal(err)
	}
	tableModelUpdate := new(TestTableFieldCommentUpdate)
	if err = dm.AutoMigrate(tableModelUpdate); err != nil {
		t.Error(err)
	}

	if m, ok := dm.(sqlserver.Migrator); ok {
		stmt := db.Model(tableModelUpdate).Find(nil).Statement
		if stmt == nil || stmt.Schema == nil {
			t.Fatal("expected Statement.Schema, got nil")
		}

		wantComments := []string{"ID", "姓名", "周岁", "生日", "注释中包含'单引号'和特殊符号❤️"}
		gotComments := make([]string, len(stmt.Schema.DBNames))

		for i, fieldDBName := range stmt.Schema.DBNames {
			comment := m.GetColumnComment(stmt, fieldDBName)
			gotComments[i] = comment.String
		}

		if !reflect.DeepEqual(wantComments, gotComments) {
			t.Fatalf("expected comments %#v, got %#v", wantComments, gotComments)
		}
		t.Logf("got comments: %#v", gotComments)
	}
}

func TestMigrator_GetIndexes(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}
	dm := db.Debug().Migrator()

	type testTableIndex struct {
		Test uint64 `gorm:"index"`
	}
	type testTableUnique struct {
		ID string `gorm:"index:unique_id,class:UNIQUE,where:id IS NOT NULL"`
	}
	type testTablePrimaryKey struct {
		ID string `gorm:"primaryKey"`
	}

	type args struct {
		value interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "index", args: args{value: new(testTableIndex)}},
		{name: "unique", args: args{value: new(testTableUnique)}},
		{name: "primaryKey", args: args{value: new(testTablePrimaryKey)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err = dm.AutoMigrate(tt.args.value); err != nil {
				t.Error(err)
			}
			got, gotErr := dm.GetIndexes(tt.args.value)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("GetIndexes() error = %v, wantErr %v", gotErr, tt.wantErr)
				return
			}
			for _, index := range got {
				_, validUnique := index.Unique()
				_, validPK := index.PrimaryKey()
				indexBytes, _ := json.Marshal(index)
				if index.Name() == "" && !validUnique && !validPK {
					t.Errorf("GetIndexes() got = %s empty", indexBytes)
				} else {
					t.Logf("GetIndexes() got = %s", indexBytes)
				}
			}
		})
	}
}
