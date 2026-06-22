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

type InsertStmt struct{}
type UpdateStmt struct{}
type DeleteStmt struct{}
type CreateStmt struct{}
type DropStmt struct{}
type AlterStmt struct{}

func (s *SelectStmt) stmtNode() {}
func (s *InsertStmt) stmtNode() {}
func (s *UpdateStmt) stmtNode() {}
func (s *DeleteStmt) stmtNode() {}
func (s *CreateStmt) stmtNode() {}
func (s *DropStmt) stmtNode()   {}
func (s *AlterStmt) stmtNode()  {}
