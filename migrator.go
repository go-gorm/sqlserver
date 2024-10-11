package sqlserver

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

const indexSQL = `
SELECT 
	i.name AS index_name,
	i.is_unique,
	i.is_primary_key,
	col.name AS column_name
FROM
	sys.indexes i
	LEFT JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
	LEFT JOIN sys.all_columns col ON col.column_id = ic.column_id AND col.object_id = ic.object_id
WHERE 
	i.name IS NOT NULL
	AND i.is_unique_constraint = 0
	AND i.object_id = OBJECT_ID(?)
`

type Migrator struct {
	migrator.Migrator
}

func (m Migrator) GetTables() (tableList []string, err error) {
	return tableList, m.DB.Raw("SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE  TABLE_CATALOG = ?", m.CurrentDatabase()).Scan(&tableList).Error
}

func (m Migrator) CreateTable(values ...interface{}) (err error) {
	if err = m.Migrator.CreateTable(values...); err != nil {
		return
	}
	for _, value := range m.ReorderModels(values, false) {
		if err = m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
			if stmt.Schema == nil {
				return
			}
			for _, fieldName := range stmt.Schema.DBNames {
				field := stmt.Schema.FieldsByDBName[fieldName]
				if field.Comment == "" {
					continue
				}
				if err = m.setColumnComment(stmt, field, true); err != nil {
					return
				}
			}
			return
		}); err != nil {
			return
		}
	}
	return
}

func (m Migrator) setColumnComment(stmt *gorm.Statement, field *schema.Field, add bool) error {
	schemaName := m.getTableSchemaName(stmt.Schema)
	// add field comment
	if add {
		return m.DB.Exec(
			"EXEC sp_addextendedproperty 'MS_Description', ?, 'SCHEMA', ?, 'TABLE', ?, 'COLUMN', ?",
			field.Comment, schemaName, stmt.Table, field.DBName,
		).Error
	}
	// update field comment
	return m.DB.Exec(
		"EXEC sp_updateextendedproperty 'MS_Description', ?, 'SCHEMA', ?, 'TABLE', ?, 'COLUMN', ?",
		field.Comment, schemaName, stmt.Table, field.DBName,
	).Error
}

func (m Migrator) getTableSchemaName(schema *schema.Schema) string {
	// return the schema name if it is explicitly provided in the table name
	// otherwise return default schema name
	schemaName := getTableSchemaName(schema)
	if schemaName == "" {
		schemaName = m.DefaultSchema()
	}
	return schemaName
}

func getTableSchemaName(schema *schema.Schema) string {
	// return the schema name if it is explicitly provided in the table name
	// otherwise return a sql wildcard -> use any table_schema
	if schema == nil || !strings.Contains(schema.Table, ".") {
		return ""
	}
	_, schemaName, _ := splitFullQualifiedName(schema.Table)
	return schemaName
}

func splitFullQualifiedName(name string) (string, string, string) {
	nameParts := strings.Split(name, ".")
	if len(nameParts) == 1 { // [table_name]
		return "", "", nameParts[0]
	} else if len(nameParts) == 2 { // [table_schema].[table_name]
		return "", nameParts[0], nameParts[1]
	} else if len(nameParts) == 3 { // [table_catalog].[table_schema].[table_name]
		return nameParts[0], nameParts[1], nameParts[2]
	}
	return "", "", ""
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
			"SELECT count(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = ? AND TABLE_CATALOG = ? and TABLE_SCHEMA like ?  AND TABLE_TYPE = ?",
			stmt.Table, m.CurrentDatabase(), schemaName, "BASE TABLE",
		).Row().Scan(&count)
	})
	return count > 0
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

func (m Migrator) AddColumn(value interface{}, name string) error {
	if err := m.Migrator.AddColumn(value, name); err != nil {
		return err
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				if field.Comment == "" {
					return
				}
				if err = m.setColumnComment(stmt, field, true); err != nil {
					return
				}
			}
		}
		return
	})
}

func (m Migrator) HasColumn(value interface{}, field string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		currentDatabase := m.DB.Migrator().CurrentDatabase()
		name := field
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(field); field != nil {
				name = field.DBName
			}
		}

		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?",
			currentDatabase, stmt.Table, name,
		).Row().Scan(&count)
	})

	return count > 0
}

func (m Migrator) AlterColumn(value interface{}, field string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(field); field != nil {
				fieldType := clause.Expr{SQL: m.DataTypeOf(field)}
				if field.NotNull {
					fieldType.SQL += " NOT NULL"
				} else {
					fieldType.SQL += " NULL"
				}

				return m.DB.Exec(
					"ALTER TABLE ? ALTER COLUMN ? ?",
					clause.Table{Name: getFullQualifiedTableName(stmt)}, clause.Column{Name: field.DBName}, fieldType,
				).Error
			}
		}
		return fmt.Errorf("failed to look up field with name: %s", field)
	})
}

func (m Migrator) RenameColumn(value interface{}, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(oldName); field != nil {
				oldName = field.DBName
			}
			if field := stmt.Schema.LookUpField(newName); field != nil {
				newName = field.DBName
			}
		}

		return m.DB.Exec(
			"sp_rename @objname = ?, @newname = ?, @objtype = 'COLUMN';",
			fmt.Sprintf("%s.%s", stmt.Table, oldName), clause.Column{Name: newName},
		).Error
	})
}

func (m Migrator) GetColumnComment(stmt *gorm.Statement, fieldDBName string) (description string) {
	queryTx := m.DB
	if m.DB.DryRun {
		queryTx = m.DB.Session(&gorm.Session{})
		queryTx.DryRun = false
	}
	var comment sql.NullString
	queryTx.Raw("SELECT value FROM ?.sys.fn_listextendedproperty('MS_Description', 'SCHEMA', ?, 'TABLE', ?, 'COLUMN', ?)",
		gorm.Expr(m.CurrentDatabase()), m.getTableSchemaName(stmt.Schema), stmt.Table, fieldDBName).Scan(&comment)
	if comment.Valid {
		description = comment.String
	}
	return
}

func (m Migrator) MigrateColumn(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	if err := m.Migrator.MigrateColumn(value, field, columnType); err != nil {
		return err
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
		description := m.GetColumnComment(stmt, field.DBName)
		if field.Comment != description {
			if description == "" {
				err = m.setColumnComment(stmt, field, true)
			} else {
				err = m.setColumnComment(stmt, field, false)
			}
		}
		return
	})
}

var defaultValueTrimRegexp = regexp.MustCompile("^\\('?([^']*)'?\\)$")

// ColumnTypes return columnTypes []gorm.ColumnType and execErr error
func (m Migrator) ColumnTypes(value interface{}) ([]gorm.ColumnType, error) {
	columnTypes := make([]gorm.ColumnType, 0)
	execErr := m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
		rows, err := m.DB.Session(&gorm.Session{}).Table(getFullQualifiedTableName(stmt)).Limit(1).Rows()
		if err != nil {
			return err
		}

		rawColumnTypes, _ := rows.ColumnTypes()
		rows.Close()

		{
			_, schemaName, tableName := splitFullQualifiedName(stmt.Table)

			query := "SELECT COLUMN_NAME, DATA_TYPE, COLUMN_DEFAULT, IS_NULLABLE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_PRECISION_RADIX, NUMERIC_SCALE, DATETIME_PRECISION FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = ? AND TABLE_NAME = ?"

			queryParameters := []interface{}{m.CurrentDatabase(), tableName}

			if schemaName != "" {
				query += " AND TABLE_SCHEMA = ?"
				queryParameters = append(queryParameters, schemaName)
			}

			var (
				columnTypeSQL   = query
				columns, rowErr = m.DB.Raw(columnTypeSQL, queryParameters...).Rows()
			)

			if rowErr != nil {
				return rowErr
			}

			for columns.Next() {
				var (
					column = migrator.ColumnType{
						PrimaryKeyValue: sql.NullBool{Valid: true},
						UniqueValue:     sql.NullBool{Valid: true},
					}
					datetimePrecision sql.NullInt64
					radixValue        sql.NullInt64
					nullableValue     sql.NullString
					values            = []interface{}{
						&column.NameValue, &column.ColumnTypeValue, &column.DefaultValueValue, &nullableValue, &column.LengthValue, &column.DecimalSizeValue, &radixValue, &column.ScaleValue, &datetimePrecision,
					}
				)

				if scanErr := columns.Scan(values...); scanErr != nil {
					return scanErr
				}

				if nullableValue.Valid {
					column.NullableValue = sql.NullBool{Bool: strings.EqualFold(nullableValue.String, "YES"), Valid: true}
				}

				if datetimePrecision.Valid {
					column.DecimalSizeValue = datetimePrecision
				}

				if column.DefaultValueValue.Valid {
					matches := defaultValueTrimRegexp.FindStringSubmatch(column.DefaultValueValue.String)
					for len(matches) > 1 {
						column.DefaultValueValue.String = matches[1]
						matches = defaultValueTrimRegexp.FindStringSubmatch(column.DefaultValueValue.String)
					}
				}

				for _, c := range rawColumnTypes {
					if c.Name() == column.NameValue.String {
						column.SQLColumnType = c
						break
					}
				}

				columnTypes = append(columnTypes, column)
			}

			columns.Close()
		}

		{
			_, schemaName, tableName := splitFullQualifiedName(stmt.Table)
			query := "SELECT c.COLUMN_NAME, t.CONSTRAINT_TYPE FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS t JOIN INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE c ON c.CONSTRAINT_NAME=t.CONSTRAINT_NAME WHERE t.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE') AND c.TABLE_CATALOG = ? AND c.TABLE_NAME = ?"

			queryParameters := []interface{}{m.CurrentDatabase(), tableName}

			if schemaName != "" {
				query += " AND c.TABLE_SCHEMA = ?"
				queryParameters = append(queryParameters, schemaName)
			}

			columnTypeRows, err := m.DB.Raw(query, queryParameters...).Rows()
			if err != nil {
				return err
			}

			for columnTypeRows.Next() {
				var name, columnType string
				columnTypeRows.Scan(&name, &columnType)
				for idx, c := range columnTypes {
					mc := c.(migrator.ColumnType)
					if mc.NameValue.String == name {
						switch columnType {
						case "PRIMARY KEY":
							mc.PrimaryKeyValue = sql.NullBool{Bool: true, Valid: true}
						case "UNIQUE":
							mc.UniqueValue = sql.NullBool{Bool: true, Valid: true}
						}
						columnTypes[idx] = mc
						break
					}
				}
			}

			columnTypeRows.Close()
		}

		return
	})

	return columnTypes, execErr
}

func (m Migrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var idx *schema.Index
		if stmt.Schema != nil {
			idx = stmt.Schema.LookIndex(name)
		}
		if idx == nil {
			return fmt.Errorf("failed to create index with name %s", name)
		}

		opts := m.BuildIndexOptions(idx.Fields, stmt)
		values := []interface{}{clause.Column{Name: idx.Name}, m.CurrentTable(stmt), opts}

		createIndexSQL := "CREATE "
		if idx.Class != "" {
			createIndexSQL += idx.Class + " "
		}
		createIndexSQL += "INDEX ? ON ??"

		if idx.Where != "" {
			createIndexSQL += " WHERE " + idx.Where
		}

		if idx.Option != "" {
			createIndexSQL += " " + idx.Option
		}

		return m.DB.Exec(createIndexSQL, values...).Error
	})
}

func (m Migrator) HasIndex(value interface{}, name string) bool {
	var count int
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
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

type Index struct {
	TableName    string
	ColumnName   string
	IndexName    string
	IsUnique     sql.NullBool
	IsPrimaryKey sql.NullBool
}

func (m Migrator) GetIndexes(value interface{}) ([]gorm.Index, error) {
	indexes := make([]gorm.Index, 0)
	err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
		result := make([]*Index, 0)
		if err := m.DB.Raw(indexSQL, stmt.Table).Scan(&result).Error; err != nil {
			return err
		}
		indexMap := make(map[string]*migrator.Index)
		for _, r := range result {
			idx, ok := indexMap[r.IndexName]
			if !ok {
				idx = &migrator.Index{
					TableName:       stmt.Table,
					NameValue:       r.IndexName,
					ColumnList:      nil,
					PrimaryKeyValue: r.IsPrimaryKey,
					UniqueValue:     r.IsUnique,
				}
			}
			idx.ColumnList = append(idx.ColumnList, r.ColumnName)
			indexMap[r.IndexName] = idx
		}
		for _, idx := range indexMap {
			indexes = append(indexes, idx)
		}
		return nil
	})
	return indexes, err
}

func (m Migrator) HasConstraint(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, table := m.GuessConstraintInterfaceAndTable(stmt, name)
		if constraint != nil {
			name = constraint.GetName()
		}

		tableCatalog, schema, tableName := splitFullQualifiedName(table)
		if tableCatalog == "" {
			tableCatalog = m.CurrentDatabase()
		}
		if schema == "" {
			schema = "%"
		}

		return m.DB.Raw(
			`SELECT count(*) FROM sys.foreign_keys as F inner join sys.tables as T on F.parent_object_id=T.object_id inner join INFORMATION_SCHEMA.TABLES as I on I.TABLE_NAME = T.name WHERE F.name = ?  AND I.TABLE_NAME = ? AND I.TABLE_SCHEMA like ? AND I.TABLE_CATALOG = ?;`,
			name, tableName, schema, tableCatalog,
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
