package sqlserver

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Migrator struct {
	migrator.Migrator
}

func (m Migrator) GetTables() (tableList []string, err error) {
	return tableList, m.DB.Raw("SELECT table_name FROM INFORMATION_SCHEMA.tables WHERE  table_catalog = ?", m.CurrentDatabase()).Scan(&tableList).Error
}

func getTableSchemaName(schema *schema.Schema) string {
	//return the schema name if it is explicitly provided in the table name
	//otherwise return a sql wildcard -> use any table_schema
	if schema == nil || !strings.Contains(schema.Table, ".") {
		return ""
	}
	tables := strings.Split(schema.Table, ".")
	if len(tables) == 2 { //[table_schema].[table_name]
		return tables[0]
	} else if len(tables) == 3 { //[table_catalog].[table_schema].[table_name]
		return tables[1]
	}

	return ""
}

func getFullQualifiedTableName(stmt *gorm.Statement) string {
	fullQualifiedTableName := stmt.Table
	if schemaName := getTableSchemaName(stmt.Schema); schemaName != "" {
		fullQualifiedTableName = schemaName + "." + fullQualifiedTableName
	}
	return fullQualifiedTableName
}

func (m Migrator) HasTable(value interface{}) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		schemaName := getTableSchemaName(stmt.Schema)
		if schemaName == "" {
			schemaName = "%"
		}
		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.tables WHERE table_name = ? AND table_catalog = ? and table_schema like ?  AND table_type = ?",
			stmt.Table, m.CurrentDatabase(), schemaName, "BASE TABLE",
		).Row().Scan(&count)
	})
	return count > 0
}

// ColumnTypes return columnTypes []gorm.ColumnType and execErr error
func (m Migrator) ColumnTypes(value interface{}) ([]gorm.ColumnType, error) {
	columnTypes := make([]gorm.ColumnType, 0)
	execErr := m.RunWithValue(value, func(stmt *gorm.Statement) error {

		rows, err := m.DB.Session(&gorm.Session{}).Table(getFullQualifiedTableName(stmt)).Limit(1).Rows()
		if err != nil {
			return err
		}

		defer rows.Close()

		var rawColumnTypes []*sql.ColumnType
		rawColumnTypes, err = rows.ColumnTypes()
		if err != nil {
			return err
		}

		for _, c := range rawColumnTypes {
			columnTypes = append(columnTypes, c)
		}

		return nil
	})

	return columnTypes, execErr
}

func (m Migrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	for i := len(values) - 1; i >= 0; i-- {
		tx := m.DB.Session(&gorm.Session{})
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			type constraint struct {
				Name   string
				Parent string
			}
			var constraints []constraint
			err := tx.Raw("SELECT name, OBJECT_NAME(parent_object_id) as parent FROM sys.foreign_keys WHERE referenced_object_id = object_id(?)", getFullQualifiedTableName(stmt)).Scan(&constraints).Error

			for _, c := range constraints {
				if err == nil {
					err = tx.Exec("ALTER TABLE ? DROP CONSTRAINT ?;", gorm.Expr(c.Parent), gorm.Expr(c.Name)).Error
				}
			}

			if err == nil {
				err = tx.Exec("DROP TABLE IF EXISTS ?", clause.Table{Name: stmt.Table}).Error
			}

			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) RenameTable(oldName, newName interface{}) error {
	var oldTable, newTable string
	if v, ok := oldName.(string); ok {
		oldTable = v
	} else {
		stmt := &gorm.Statement{DB: m.DB}
		if err := stmt.Parse(oldName); err == nil {
			oldTable = stmt.Table
		} else {
			return err
		}
	}

	if v, ok := newName.(string); ok {
		newTable = v
	} else {
		stmt := &gorm.Statement{DB: m.DB}
		if err := stmt.Parse(newName); err == nil {
			newTable = stmt.Table
		} else {
			return err
		}
	}

	return m.DB.Exec(
		"sp_rename @objname = ?, @newname = ?;",
		clause.Table{Name: oldTable}, clause.Table{Name: newTable},
	).Error
}

func (m Migrator) HasColumn(value interface{}, field string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		currentDatabase := m.DB.Migrator().CurrentDatabase()
		name := field
		if field := stmt.Schema.LookUpField(field); field != nil {
			name = field.DBName
		}

		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.columns WHERE table_catalog = ? AND table_name = ? AND column_name = ?",
			currentDatabase, stmt.Table, name,
		).Row().Scan(&count)
	})

	return count > 0
}

func (m Migrator) AlterColumn(value interface{}, field string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(field); field != nil {
			fileType := clause.Expr{SQL: m.DataTypeOf(field)}
			if field.NotNull {
				fileType.SQL += " NOT NULL"
			}

			return m.DB.Exec(
				"ALTER TABLE ? ALTER COLUMN ? ?",
				clause.Table{Name: stmt.Table}, clause.Column{Name: field.DBName}, fileType,
			).Error
		}
		return fmt.Errorf("failed to look up field with name: %s", field)
	})
}

func (m Migrator) RenameColumn(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(oldName); field != nil {
			oldName = field.DBName
		}

		if field := stmt.Schema.LookUpField(newName); field != nil {
			newName = field.DBName
		}

		return m.DB.Exec(
			"sp_rename @objname = ?, @newname = ?, @objtype = 'COLUMN';",
			fmt.Sprintf("%s.%s", stmt.Table, oldName), clause.Column{Name: newName},
		).Error
	})
}

func (m Migrator) HasIndex(value interface{}, name string) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		return m.DB.Raw(
			"SELECT count(*) FROM sys.indexes WHERE name=? AND object_id=OBJECT_ID(?)",
			name, getFullQualifiedTableName(stmt),
		).Row().Scan(&count)
	})
	return count > 0
}

func (m Migrator) RenameIndex(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {

		return m.DB.Exec(
			"sp_rename @objname = ?, @newname = ?, @objtype = 'INDEX';",
			fmt.Sprintf("%s.%s", stmt.Table, oldName), clause.Column{Name: newName},
		).Error
	})
}

func (m Migrator) HasConstraint(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, chk, table := m.GuessConstraintAndTable(stmt, name)
		if constraint != nil {
			name = constraint.Name
		} else if chk != nil {
			name = chk.Name
		}

		return m.DB.Raw(
			`SELECT count(*) FROM sys.foreign_keys as F inner join sys.tables as T on F.parent_object_id=T.object_id inner join information_schema.tables as I on I.TABLE_NAME = T.name WHERE F.name = ?  AND T.object_id = OBJECT_ID(?) AND I.TABLE_CATALOG = ?;`,
			name, table, m.CurrentDatabase(),
		).Row().Scan(&count)
	})
	return count > 0
}

func (m Migrator) CurrentDatabase() (name string) {
	m.DB.Raw("SELECT DB_NAME() AS [Current Database]").Row().Scan(&name)
	return
}

func (m Migrator) DefaultSchema() (name string) {
	m.DB.Raw("SELECT SCHEMA_NAME() AS [Default Schema]").Row().Scan(&name)
	return
}
