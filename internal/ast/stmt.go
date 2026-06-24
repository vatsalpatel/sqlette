package ast

type SelectStmt struct {
	Columns []ResultColumn
	From    TableRef
	Where   Expression
}

type ResultColumn struct {
	Expr  Expression
	Alias string
}

type TableRef struct {
	Name  string
	Alias string
}

type InsertStmt struct {
	Table   string
	Columns []ColumnRef
	Rows    [][]Expression
}

type UpdateStmt struct{}
type DeleteStmt struct{}

type CreateStmt struct {
	Table   string
	Columns []ColumnDef
}

type ColumnDef struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

type DropStmt struct{}
type AlterStmt struct{}

func (s *SelectStmt) stmtNode() {}
func (s *InsertStmt) stmtNode() {}
func (s *UpdateStmt) stmtNode() {}
func (s *DeleteStmt) stmtNode() {}
func (s *CreateStmt) stmtNode() {}
func (s *DropStmt) stmtNode()   {}
func (s *AlterStmt) stmtNode()  {}
