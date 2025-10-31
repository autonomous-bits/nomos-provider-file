package provider

import (
	"fmt"
	"os"

	"github.com/autonomous-bits/nomos/libs/parser"
	"github.com/autonomous-bits/nomos/libs/parser/pkg/ast"
)

// parseCSLFile parses a .csl file and returns its data as a map[string]any.
func parseCSLFile(filePath string) (any, error) {
	// Parse the .csl file using the public parser API
	tree, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Convert AST to data structure
	data, err := astToData(tree)
	if err != nil {
		return nil, fmt.Errorf("conversion error: %w", err)
	}

	return data, nil
}

// astToData converts an AST to a data structure (map[string]any).
// This is a simplified converter that handles the basic Nomos constructs.
func astToData(tree *ast.AST) (map[string]any, error) {
	result := make(map[string]any)

	for _, stmt := range tree.Statements {
		switch s := stmt.(type) {
		case *ast.SectionDecl:
			sectionData := make(map[string]any)
			for key, expr := range s.Entries {
				value, err := convertExpr(expr)
				if err != nil {
					return nil, fmt.Errorf("failed to convert value for key %q in section %q: %w", key, s.Name, err)
				}
				sectionData[key] = value
			}
			result[s.Name] = sectionData

		// Skip source and import declarations - these are metadata
		case *ast.SourceDecl, *ast.ImportStmt:
			continue
		}
	}

	return result, nil
}

// convertExpr converts an AST expression to a Go value.
func convertExpr(expr ast.Expr) (any, error) {
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return e.Value, nil

	case *ast.ReferenceExpr:
		// References cannot be resolved in the provider - return a placeholder
		// The compiler will resolve these
		pathStr := ""
		for i, p := range e.Path {
			if i > 0 {
				pathStr += "."
			}
			pathStr += p
		}
		return fmt.Sprintf("reference:%s:%s", e.Alias, pathStr), nil

	case *ast.IdentExpr:
		// Identifiers as values (e.g., boolean true/false or unquoted strings)
		// For now, return as string
		return e.Name, nil

	case *ast.PathExpr:
		// Path expressions as values
		pathStr := ""
		for i, c := range e.Components {
			if i > 0 {
				pathStr += "."
			}
			pathStr += c
		}
		return pathStr, nil

	default:
		return nil, fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// Helper to read file content as string (for debugging)
func readFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
