package sqlserver_test

import (
	"testing"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

type testSchemaUser struct {
	ID   int64 `gorm:"primaryKey"`
	Name string
}

func (testSchemaUser) TableName() string {
	return "users"
}

func setupSchemaTable(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := db.Exec("CREATE SCHEMA testschema").Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Exec("DROP TABLE IF EXISTS testschema.users").Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec("DROP SCHEMA IF EXISTS testschema").Error; err != nil {
			t.Error(err)
		}
	})

	if err := db.Exec(`
		CREATE TABLE testschema.users (
			id BIGINT IDENTITY(1,1) PRIMARY KEY,
			name NVARCHAR(100)
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
}

func assertUser(t *testing.T, db *gorm.DB, id int64, name string) {
	t.Helper()

	var got testSchemaUser
	if err := db.Table("testschema.users").First(&got, id).Error; err != nil {
		t.Fatal(err)
	}

	if got.Name != name {
		t.Fatalf("expected %q, got %q", name, got.Name)
	}
}

func TestCreateWithSchemaTable(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}

	setupSchemaTable(t, db)

	if err := db.Table("testschema.users").Create(&testSchemaUser{
		ID:   1,
		Name: "gorm",
	}).Error; err != nil {
		t.Fatal(err)
	}

	assertUser(t, db, 1, "gorm")
}

func TestSaveWithSchemaTable(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}

	setupSchemaTable(t, db)

	if err := db.Table("testschema.users").Save(&testSchemaUser{
		ID:   1,
		Name: "gorm",
	}).Error; err != nil {
		t.Fatal(err)
	}

	assertUser(t, db, 1, "gorm")
}
