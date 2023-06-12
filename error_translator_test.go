package sqlserver

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/microsoft/go-mssqldb"
)

func TestDialector_Translate(t *testing.T) {
	type fields struct {
		Config *Config
	}
	type args struct {
		err error
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   error
	}{
		{
			name: "it should return ErrDuplicatedKey error if the error number is 2627",
			args: args{err: mssql.Error{Number: 2627}},
			want: gorm.ErrDuplicatedKey,
		},
		{
			name: "it should return ErrForeignKeyViolated the error number is 547",
			args: args{err: mssql.Error{Number: 547}},
			want: gorm.ErrForeignKeyViolated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialector := Dialector{
				Config: tt.fields.Config,
			}
			if err := dialector.Translate(tt.args.err); !errors.Is(err, tt.want) {
				t.Errorf("Translate() expected error = %v, got error %v", err, tt.want)
			}
		})
	}
}
