package sqlserver

import (
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func Update(db *gorm.DB) {
	if db.Statement.Schema != nil && db.Statement.Schema.PrioritizedPrimaryField != nil && db.Statement.Schema.PrioritizedPrimaryField.AutoIncrement {
		db.Statement.Omits = append(db.Statement.Omits, db.Statement.Schema.PrioritizedPrimaryField.DBName)
	}

	callbacks.Update(db)
}
