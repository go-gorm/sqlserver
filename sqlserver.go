package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Config struct {
	DriverName        string
	DSN               string
	DefaultStringSize int
	Conn              gorm.ConnPool

	serverMajorVersion int
	hasServerVersion   bool
}

type Dialector struct {
	*Config
}

func (dialector Dialector) Name() string {
	return "sqlserver"
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{Config: &Config{DSN: dsn}}
}

func New(config Config) gorm.Dialector {
	return &Dialector{Config: &config}
}

func (dialector Dialector) Initialize(db *gorm.DB) (err error) {
	// register callbacks
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		CreateClauses: []string{"INSERT", "VALUES", "ON CONFLICT"},
		QueryClauses:  []string{"SELECT", "FROM", "WHERE", "GROUP BY", "ORDER BY", "LIMIT", "FOR"},
		UpdateClauses: []string{"UPDATE", "SET", "RETURNING", "FROM", "WHERE"},
		DeleteClauses: []string{"DELETE", "FROM", "RETURNING", "WHERE"},
	})
	db.Callback().Create().Replace("gorm:create", Create)
	db.Callback().Update().Replace("gorm:update", Update)

	if dialector.DriverName == "" {
		dialector.DriverName = "sqlserver"
	}

	if dialector.Conn != nil {
		db.ConnPool = dialector.Conn
	} else {
		db.ConnPool, err = sql.Open(dialector.DriverName, dialector.DSN)
		if err != nil {
			return err
		}
	}

	if err = dialector.detectServerVersion(db); err != nil {
		// Fall back to the modern OFFSET/FETCH syntax if the version cannot be
		// determined, but keep the legacy path available for older SQL Server
		// instances.
		dialector.serverMajorVersion = 0
		dialector.hasServerVersion = false
	}

	for k, v := range dialector.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}
	return
}

func (dialector Dialector) supportsOffsetFetch() bool {
	if !dialector.hasServerVersion {
		return true
	}
	return dialector.serverMajorVersion >= 11
}

func (dialector Dialector) detectServerVersion(db *gorm.DB) error {
	if db == nil || db.ConnPool == nil {
		return nil
	}

	var version string
	if err := db.ConnPool.QueryRowContext(context.Background(),
		"SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))").Scan(&version); err != nil {
		return err
	}

	major, ok := parseSQLServerVersion(version)
	if !ok {
		return nil
	}

	dialector.serverMajorVersion = major
	dialector.hasServerVersion = true
	return nil
}

func parseSQLServerVersion(version string) (int, bool) {
	if version == "" {
		return 0, false
	}

	for _, part := range strings.FieldsFunc(version, func(ch rune) bool {
		switch ch {
		case '.', ' ', '-', '\t', '\n', '\r', ',', ';':
			return true
		default:
			return false
		}
	}) {
		major, err := strconv.Atoi(part)
		if err == nil {
			return major, true
		}
	}

	return 0, false
}

func addTopClause(query string, limit int) string {
	query = strings.TrimSpace(query)
	if limit < 0 || query == "" {
		return query
	}

	if !strings.HasPrefix(strings.ToUpper(query), "SELECT") {
		return query
	}

	return strings.Replace(query, "SELECT", fmt.Sprintf("SELECT TOP (%d)", limit), 1)
}

func buildLegacyPaginationSQL(query string, offset int, limit *int) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return query
	}

	var size int
	unlimited := limit == nil || *limit < 0
	if !unlimited {
		size = *limit
	}
	if offset == 0 {
		if unlimited {
			return query
		}
		return addTopClause(query, size)
	}

	orderByIndex := strings.Index(strings.ToUpper(query), " ORDER BY ")
	if orderByIndex >= 0 {
		orderBySQL := strings.TrimSpace(query[orderByIndex:])
		baseQuery := strings.TrimRight(query[:orderByIndex], " ")
		upperBound := ""
		if !unlimited {
			upperBound = fmt.Sprintf(" AND __gorm_rownum <= %d", offset+size)
		}
		return fmt.Sprintf(
			"SELECT * FROM (SELECT __gorm_wrapped.*, ROW_NUMBER() OVER (%s) AS __gorm_rownum FROM (%s) AS __gorm_wrapped) AS __gorm_paged WHERE __gorm_rownum > %d%s ORDER BY __gorm_rownum",
			orderBySQL,
			baseQuery,
			offset,
			upperBound,
		)
	}

	if unlimited {
		return query
	}
	return addTopClause(query, offset+size)
}

func (dialector Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	return map[string]clause.ClauseBuilder{
		"LIMIT": func(c clause.Clause, builder clause.Builder) {
			if limit, ok := c.Expression.(clause.Limit); ok {
				if stmt, ok := builder.(*gorm.Statement); ok {
					if _, ok := stmt.Clauses["ORDER BY"]; !ok {
						if stmt.Schema != nil && stmt.Schema.PrioritizedPrimaryField != nil {
							builder.WriteString("ORDER BY ")
							builder.WriteQuoted(stmt.Schema.PrioritizedPrimaryField.DBName)
							builder.WriteByte(' ')
						} else {
							builder.WriteString("ORDER BY (SELECT NULL) ")
						}
					}

					if !dialector.supportsOffsetFetch() {
						rewritten := buildLegacyPaginationSQL(stmt.SQL.String(), limit.Offset, limit.Limit)
						stmt.SQL.Reset()
						stmt.SQL.WriteString(rewritten)
						return
					}

					if limit.Offset > 0 {
						builder.WriteString("OFFSET ")
						builder.WriteString(strconv.Itoa(limit.Offset))
						builder.WriteString(" ROWS")
					}

					if limit.Limit != nil && *limit.Limit >= 0 {
						if limit.Offset == 0 {
							builder.WriteString("OFFSET 0 ROW")
						}
						builder.WriteString(" FETCH NEXT ")
						builder.WriteString(strconv.Itoa(*limit.Limit))
						builder.WriteString(" ROWS ONLY")
					}
				}
			}
		},
		"RETURNING": func(c clause.Clause, builder clause.Builder) {
			if returning, ok := c.Expression.(clause.Returning); ok {
				if stmt, ok := builder.(*gorm.Statement); ok {
					var outputTable string
					if _, ok := stmt.Clauses["UPDATE"]; ok {
						outputTable = "INSERTED"
					} else if _, ok := stmt.Clauses["DELETE"]; ok {
						outputTable = "DELETED"
					}

					if outputTable != "" {
						stmt.WriteString("OUTPUT ")

						if len(returning.Columns) > 0 {
							columns := []clause.Column{}
							for _, column := range returning.Columns {
								column.Table = outputTable
								columns = append(columns, column)
							}
							returning.Columns = columns
							returning.Build(stmt)
						} else {
							stmt.WriteString(outputTable + ".*")
						}
					}
				}
			}
		},
	}
}

func (dialector Dialector) DefaultValueOf(field *schema.Field) clause.Expression {
	return clause.Expr{SQL: "NULL"}
}

func (dialector Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   dialector,
		CreateIndexAfterCreateTable: true,
	}}}
}

func (dialector Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteString("@p")
	writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (dialector Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteByte('"')
	if strings.Contains(str, ".") {
		for idx, str := range strings.Split(str, ".") {
			if idx > 0 {
				writer.WriteString(`."`)
			}
			writer.WriteString(str)
			writer.WriteByte('"')
		}
	} else {
		writer.WriteString(str)
		writer.WriteByte('"')
	}
}

var numericPlaceholder = regexp.MustCompile("@p(\\d+)")

func (dialector Dialector) Explain(sql string, vars ...interface{}) string {
	for idx, v := range vars {
		if b, ok := v.(bool); ok {
			if b {
				vars[idx] = 1
			} else {
				vars[idx] = 0
			}
		}
	}

	return logger.ExplainSQL(sql, numericPlaceholder, `'`, vars...)
}

func (dialector Dialector) DataTypeOf(field *schema.Field) string {
	// Computed (generated) column. SQL Server uses `AS (expr) PERSISTED` (the
	// column type is inferred from the expression, so it is omitted). The
	// expression is carried by the `generated` tag, separate from the type.
	// https://learn.microsoft.com/en-us/sql/relational-databases/tables/specify-computed-columns-in-a-table
	if expr, ok := generatedColumnExpr(field); ok {
		return "AS (" + expr + ") PERSISTED"
	}

	return dialector.dataTypeOf(field)
}

func (dialector Dialector) dataTypeOf(field *schema.Field) string {
	switch field.DataType {
	case schema.Bool:
		return "bit"
	case schema.Int, schema.Uint:
		var sqlType string
		switch {
		case field.Size < 16:
			sqlType = "smallint"
		case field.Size < 31:
			sqlType = "int"
		default:
			sqlType = "bigint"
		}

		if field.AutoIncrement {
			return sqlType + " IDENTITY(1,1)"
		}
		return sqlType
	case schema.Float:
		if field.Precision > 0 {
			if field.Scale > 0 {
				return fmt.Sprintf("decimal(%d, %d)", field.Precision, field.Scale)
			}
			return fmt.Sprintf("decimal(%d)", field.Precision)
		}
		return "float"
	case schema.String:
		size := field.Size
		hasIndex := field.TagSettings["INDEX"] != "" || field.TagSettings["UNIQUE"] != ""
		if (field.PrimaryKey || hasIndex) && size == 0 {
			if dialector.DefaultStringSize > 0 {
				size = dialector.DefaultStringSize
			} else {
				size = 256
			}
		}
		if size > 0 && size <= 4000 {
			return fmt.Sprintf("nvarchar(%d)", size)
		}
		return "nvarchar(MAX)"
	case schema.Time:
		if field.Precision > 0 {
			return fmt.Sprintf("datetimeoffset(%d)", field.Precision)
		}
		return "datetimeoffset"
	case schema.Bytes:
		return "varbinary(MAX)"
	}

	return string(field.DataType)
}

func (dialectopr Dialector) SavePoint(tx *gorm.DB, name string) error {
	tx.Exec("SAVE TRANSACTION " + name)
	return nil
}

func (dialectopr Dialector) RollbackTo(tx *gorm.DB, name string) error {
	tx.Exec("ROLLBACK TRANSACTION " + name)
	return nil
}

// generatedColumnExpr returns the expression of a computed (generated) column
// declared via the `generated` tag, if any. The `identity` keyword is reserved
// for identity columns (rendered through the dialect's native IDENTITY) and is
// not a computed-column expression.
func generatedColumnExpr(field *schema.Field) (string, bool) {
	value, ok := field.TagSettings["GENERATED"]
	if !ok {
		return "", false
	}
	// Ignore an empty value or a bare `generated` tag, which the tag parser
	// stores as the upper-cased key, rather than treating it as an expression.
	if value = strings.TrimSpace(value); value == "" || value == "GENERATED" {
		return "", false
	}
	if isIdentityKeyword(value) {
		return "", false
	}
	return value, true
}

// isIdentityKeyword reports whether value is the `identity` keyword, optionally
// combined with the generation mode `always` / `by default`. Any other token
// means value is a computed-column expression.
func isIdentityKeyword(value string) bool {
	identity := false
	for _, token := range strings.Fields(strings.ToLower(value)) {
		switch token {
		case "identity":
			identity = true
		case "always", "by", "default":
		default:
			return false
		}
	}
	return identity
}
