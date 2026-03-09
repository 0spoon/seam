// Package note implements note domain logic: CRUD, parsing, and indexing.
package note

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// knownFrontmatterKeys lists the YAML keys that map to typed struct fields.
// Any YAML key NOT in this set is preserved in the Extra map.
var knownFrontmatterKeys = map[string]bool{
	"id":                true,
	"title":             true,
	"project":           true,
	"tags":              true,
	"created":           true,
	"modified":          true,
	"source_url":        true,
	"transcript_source": true,
}

// Frontmatter represents the YAML frontmatter as it exists on disk.
// The Extra field preserves unknown YAML keys so that external tools
// (e.g., Obsidian plugins) do not lose their custom fields on round-trip.
type Frontmatter struct {
	ID               string                 `yaml:"id"`
	Title            string                 `yaml:"title"`
	Project          string                 `yaml:"project,omitempty"`
	Tags             []string               `yaml:"tags,omitempty"`
	Created          time.Time              `yaml:"created"`
	Modified         time.Time              `yaml:"modified"`
	SourceURL        string                 `yaml:"source_url,omitempty"`
	TranscriptSource bool                   `yaml:"transcript_source,omitempty"`
	Extra            map[string]interface{} `yaml:"-"`
}

// UnmarshalYAML implements custom unmarshaling that captures unknown fields
// into the Extra map while populating typed struct fields normally.
func (fm *Frontmatter) UnmarshalYAML(value *yaml.Node) error {
	// First, unmarshal into the typed fields using a type alias to
	// avoid infinite recursion.
	type plain Frontmatter
	if err := value.Decode((*plain)(fm)); err != nil {
		return err
	}

	// Then, unmarshal into a raw map to discover unknown keys.
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return err
	}

	for key, val := range raw {
		if !knownFrontmatterKeys[key] {
			if fm.Extra == nil {
				fm.Extra = make(map[string]interface{})
			}
			fm.Extra[key] = val
		}
	}

	return nil
}

// MarshalYAML implements custom marshaling that includes Extra fields
// alongside the typed struct fields.
func (fm *Frontmatter) MarshalYAML() (interface{}, error) {
	// Build an ordered node so known fields come first in a consistent
	// order, followed by extra fields sorted alphabetically.
	node := &yaml.Node{Kind: yaml.MappingNode}

	addField := func(key, value string) {
		if value == "" {
			return
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value},
		)
	}

	addField("id", fm.ID)
	addField("title", fm.Title)
	addField("project", fm.Project)

	// Tags.
	if len(fm.Tags) > 0 {
		tagNode := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, t := range fm.Tags {
			tagNode.Content = append(tagNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: t},
			)
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "tags"},
			tagNode,
		)
	}

	if !fm.Created.IsZero() {
		addField("created", fm.Created.Format(time.RFC3339))
	}
	if !fm.Modified.IsZero() {
		addField("modified", fm.Modified.Format(time.RFC3339))
	}
	addField("source_url", fm.SourceURL)
	if fm.TranscriptSource {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "transcript_source"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "true"},
		)
	}

	// Append extra fields in sorted order.
	if len(fm.Extra) > 0 {
		keys := make([]string, 0, len(fm.Extra))
		for k := range fm.Extra {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
			valNode := &yaml.Node{}
			if err := valNode.Encode(fm.Extra[k]); err != nil {
				return nil, fmt.Errorf("note.Frontmatter.MarshalYAML: encode extra %q: %w", k, err)
			}
			node.Content = append(node.Content, keyNode, valNode)
		}
	}

	return node, nil
}

// sortStrings sorts a string slice in place (avoids importing "sort" for a trivial case).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ParseFrontmatter splits a markdown file into its YAML frontmatter and body.
// Returns the parsed Frontmatter, the body text (without frontmatter delimiters),
// and any error.
//
// Frontmatter is delimited by "---" lines at the start of the file.
// If there is no frontmatter, an empty Frontmatter is returned with the
// entire content as the body.
func ParseFrontmatter(content string) (*Frontmatter, string, error) {
	fm := &Frontmatter{}

	// Normalize line endings.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Must start with "---\n".
	if !strings.HasPrefix(content, "---\n") {
		return fm, content, nil
	}

	// Find the closing "---".
	rest := content[4:] // skip "---\n"

	closingIdx := strings.Index(rest, "\n---")
	if closingIdx == -1 {
		// Check if the closing delimiter is at the very start (empty frontmatter).
		if strings.HasPrefix(rest, "---\n") || rest == "---" {
			body := strings.TrimPrefix(rest, "---")
			if len(body) > 0 && body[0] == '\n' {
				body = body[1:]
			}
			return fm, body, nil
		}
		// No closing delimiter: treat entire content as body.
		return fm, content, nil
	}

	yamlContent := rest[:closingIdx]
	body := rest[closingIdx+4:] // skip "\n---"

	// Strip leading newline from body.
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	if err := yaml.Unmarshal([]byte(yamlContent), fm); err != nil {
		return nil, "", fmt.Errorf("note.ParseFrontmatter: %w", err)
	}

	return fm, body, nil
}

// SerializeFrontmatter renders a Frontmatter struct to the full markdown
// file content, combining the YAML frontmatter and the body.
func SerializeFrontmatter(fm *Frontmatter, body string) (string, error) {
	data, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("note.SerializeFrontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(data)
	b.WriteString("---\n")
	if body != "" {
		b.WriteString(body)
		// Ensure file ends with a newline.
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}

	return b.String(), nil
}
