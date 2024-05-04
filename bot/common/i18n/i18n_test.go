package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestCompleteTranslations ensures that each language has all required text IDs translated.
func TestCompleteTranslations(t *testing.T) {
	requiredTextIDs, err := getTextsList()
	if err != nil {
		t.Errorf("Error reading the text identifers list: %s", err)
	}
	languages := []Language{EnUS, FaIR}

	for _, lang := range languages {
		// Ensure the locale is loaded for testing
		if _, ok := locales[lang]; !ok {
			locales[lang] = loadLanguage(lang)
		}

		texts, ok := locales[lang]
		if !ok {
			t.Errorf("locale for language '%s' was not loaded", lang)
			continue
		}

		for _, id := range requiredTextIDs {
			if _, exists := texts[id]; !exists {
				t.Errorf("missing text '%s' for language '%s'", id, lang)
			}
		}
	}
}

func getTextsList() ([]TextID, error) {
	filePath := "./translation.go"

	// Create a new token file set
	fset := token.NewFileSet()

	// Parse the file
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("error parsing file: %s", err)
	}

	var ids []TextID

	// Use ast.Inspect to traverse the AST and find constants
	ast.Inspect(node, func(n ast.Node) bool {
		// Check for general declarations (var, const, type)
		genDecl, ok := n.(*ast.GenDecl)
		if ok && genDecl.Tok == token.CONST {
			for _, spec := range genDecl.Specs {
				// Assertion for ValueSpec since constants belong to this category
				valSpec, ok := spec.(*ast.ValueSpec)
				if ok {
					for _, name := range valSpec.Names {
						if strings.HasSuffix(name.Name, "Text") {
							ids = append(ids, TextID(name.Name))
						}
					}
				}
			}
		}
		return true // Continue to traverse the AST
	})

	return ids, nil
}
