// Command db-prepared normalizes sqlc's repeated prepared-statement lifecycle.
// Run through go generate ./internal/db after sqlc generation.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

type statement struct{ field, query, name string }
type replacement struct {
	start, end int
	body       string
}

func main() {
	path := flag.String("file", "internal/db/db.go", "sqlc-generated DB lifecycle file")
	flag.Parse()
	raw, err := os.ReadFile(*path)
	if err == nil {
		var normalized []byte
		normalized, err = normalize(raw)
		if err == nil {
			err = os.WriteFile(*path, normalized, 0644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func normalize(raw []byte) ([]byte, error) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "db.go", raw, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var prepare, close *ast.FuncDecl
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "preparedStatements":
			return raw, nil
		case "Prepare":
			prepare = fn
		case "Close":
			close = fn
		}
	}
	if prepare == nil || close == nil {
		return nil, fmt.Errorf("sqlc prepared lifecycle functions not found")
	}
	entries, err := statementEntries(prepare)
	if err != nil {
		return nil, err
	}
	return renderNormalized(raw, fs, prepare, close, entries)
}

func statementEntries(prepare *ast.FuncDecl) ([]statement, error) {
	var entries []statement
	for _, node := range prepare.Body.List {
		condition, ok := node.(*ast.IfStmt)
		if !ok {
			continue
		}
		entry, err := statementEntry(condition)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("sqlc prepared statements not found")
	}
	return entries, nil
}

func statementEntry(condition *ast.IfStmt) (statement, error) {
	var entry statement
	assignment, ok := condition.Init.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return entry, fmt.Errorf("unsupported sqlc preparation assignment")
	}
	field, ok := assignment.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return entry, fmt.Errorf("unsupported sqlc statement field")
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return entry, fmt.Errorf("unsupported sqlc preparation call")
	}
	query, ok := call.Args[1].(*ast.Ident)
	if !ok {
		return entry, fmt.Errorf("unsupported sqlc query identifier")
	}
	entry.field, entry.query = field.Sel.Name, query.Name
	entry.name = strings.ToUpper(query.Name[:1]) + query.Name[1:]
	return entry, nil
}

func renderNormalized(raw []byte, fs *token.FileSet, prepare, close *ast.FuncDecl, entries []statement) ([]byte, error) {
	prepareBody := `{
 q := Queries{db: db}
 var err error
 for _, statement := range q.preparedStatements() {
  *statement.destination, err = db.PrepareContext(ctx, statement.query)
  if err != nil { return nil, fmt.Errorf("error preparing query %s: %w", statement.name, err) }
 }
 return &q, nil
}`
	closeBody := `{
 var err error
 for _, statement := range q.preparedStatements() {
  if *statement.destination == nil { continue }
  if closeErr := (*statement.destination).Close(); closeErr != nil {
   err = fmt.Errorf("error closing %s: %w", statement.field, closeErr)
  }
 }
 return err
}`
	changes := []replacement{{fs.Position(prepare.Body.Pos()).Offset, fs.Position(prepare.Body.End()).Offset, prepareBody}, {fs.Position(close.Body.Pos()).Offset, fs.Position(close.Body.End()).Offset, closeBody}}
	if changes[0].start < changes[1].start {
		changes[0], changes[1] = changes[1], changes[0]
	}
	result := append([]byte(nil), raw...)
	for _, change := range changes {
		result = append(append(append([]byte(nil), result[:change.start]...), []byte(change.body)...), result[change.end:]...)
	}
	var helper bytes.Buffer
	helper.WriteString("\n// preparedStatement keeps query order and the generated diagnostic names.\ntype preparedStatement struct { name, field string; destination **sql.Stmt; query string }\n")
	fmt.Fprintf(&helper, "\nfunc (q *Queries) preparedStatements() [%d]preparedStatement {\nreturn [%d]preparedStatement{\n", len(entries), len(entries))
	for _, entry := range entries {
		fmt.Fprintf(&helper, "{%s, %s, &q.%s, %s},\n", strconv.Quote(entry.name), strconv.Quote(entry.field), entry.field, entry.query)
	}
	helper.WriteString("}\n}\n")
	return format.Source(append(result, helper.Bytes()...))
}
