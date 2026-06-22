package ast

type (
	Statement  interface{ stmtNode() }
	Expression interface{ exprNode() }
)
