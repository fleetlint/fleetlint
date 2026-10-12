// Package yamlsafe decodes YAML that a repository or a catalog controls.
// Anchors and aliases stay allowed (workflows use them), but a document
// whose aliases expand to more than MaxNodes nodes is refused before it is
// decoded, so a few hundred bytes cannot turn into gigabytes of values.
package yamlsafe

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// MaxNodes bounds the nodes of a document with every alias expanded.
const MaxNodes = 200_000

// Unmarshal checks the expanded size, then decodes like
// yaml.UnmarshalWithOptions.
func Unmarshal(b []byte, v any, opts ...yaml.DecodeOption) error {
	if err := Check(b); err != nil {
		return err
	}
	return yaml.UnmarshalWithOptions(b, v, opts...)
}

// Check refuses a document whose aliases expand beyond MaxNodes. A
// document that does not parse is left to the decoder's error.
func Check(b []byte) error {
	f, err := parser.ParseBytes(b, 0)
	if err != nil {
		return nil //nolint:nilerr // the decoder reports syntax errors with positions
	}
	for _, doc := range f.Docs {
		c := counter{anchors: map[string]int{}}
		if c.size(doc.Body) > MaxNodes {
			return fmt.Errorf("YAML document expands to more than %d nodes through aliases", MaxNodes)
		}
	}
	return nil
}

// counter computes the expanded size in document order, where an alias
// always refers to an anchor defined before it.
type counter struct {
	anchors map[string]int
}

func (c *counter) size(n ast.Node) int {
	switch t := n.(type) {
	case nil:
		return 0
	case *ast.MappingNode:
		total := 1
		for _, v := range t.Values {
			total = add(total, c.size(v))
		}
		return total
	case *ast.MappingValueNode:
		return add(c.size(t.Key), c.size(t.Value))
	case *ast.SequenceNode:
		total := 1
		for _, v := range t.Values {
			total = add(total, c.size(v))
		}
		return total
	case *ast.AnchorNode:
		s := c.size(t.Value)
		c.anchors[t.Name.String()] = s
		return s
	case *ast.AliasNode:
		return c.anchors[t.Value.String()]
	case *ast.TagNode:
		return add(1, c.size(t.Value))
	case *ast.MappingKeyNode:
		return add(1, c.size(t.Value))
	}
	return 1
}

// add saturates above MaxNodes so deep nesting cannot overflow.
func add(a, b int) int {
	if a+b > MaxNodes {
		return MaxNodes + 1
	}
	return a + b
}
