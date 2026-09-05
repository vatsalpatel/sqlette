package ast

type SelectStmt struct {
	Columns []ResultColumn
	From    TableRef
	Where   Expression
	OrderBy []OrderTerm
	Limit   Expression
	Offset  Expression
}

type ResultColumn struct {
	Expr  Expression
	Alias string
}

type TableRef struct {
	Name  string
	Alias string
}

type OrderTerm struct {
	Expr Expression
	Desc bool
}

type InsertStmt struct {
	Table   string
	Columns []ColumnRef
	Rows    [][]Expression
}

type UpdateStmt struct {
	Assigns []Assign
	Table   string
	Where   Expression
}

type DeleteStmt struct {
	Table string
	Where Expression
}

type CreateStmt struct {
	Table   string
	Columns []ColumnDef
}

type CreateIndexStmt struct {
	Name    string
	Table   string
	Columns []string
	Unique  bool
}

type ColumnDef struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

type Assign struct {
	Column string
	Value  Expression
}

type DropStmt struct{}
type AlterStmt struct{}
type BeginStmt struct{}
type CommitStmt struct{}
type RollbackStmt struct{}

type ExplainStmt struct {
	Stmt Statement
}

func (s *SelectStmt) stmtNode()      {}
func (s *InsertStmt) stmtNode()      {}
func (s *UpdateStmt) stmtNode()      {}
func (s *DeleteStmt) stmtNode()      {}
func (s *CreateStmt) stmtNode()      {}
func (s *CreateIndexStmt) stmtNode() {}
func (s *DropStmt) stmtNode()        {}
func (s *AlterStmt) stmtNode()       {}
func (s *BeginStmt) stmtNode()       {}
func (s *CommitStmt) stmtNode()      {}
func (s *RollbackStmt) stmtNode()    {}
func (s *ExplainStmt) stmtNode()     {}
