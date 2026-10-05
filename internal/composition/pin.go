package composition

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

// PinChange describes the digest pin applied to one declared composition source.
type PinChange struct {
	Name      string
	OldDigest string
	NewDigest string
}

// Changed reports whether the pin rewrote the intent for this source.
func (c PinChange) Changed() bool {
	return c.OldDigest != c.NewDigest
}

// PinIntentDigests writes each resolved source digest into the `digest:` field
// of the matching compositions.sources[] entry in the intent file, so the
// existing digest check at resolution turns the lock into an enforced pin.
//
// The file is edited line by line (located via yaml.v3 node positions) rather
// than re-encoded, so comments, key order, quoting and indentation elsewhere in
// the file are left untouched. The file is only rewritten when a digest changes.
func PinIntentDigests(intentPath string, sources []model.ResolvedCompositionSource) ([]PinChange, error) {
	data, err := os.ReadFile(intentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read intent %s: %w", intentPath, err)
	}

	updated, changes, err := pinIntentDigests(data, sources)
	if err != nil {
		return nil, fmt.Errorf("failed to pin composition digests in %s: %w", intentPath, err)
	}
	if !bytes.Equal(updated, data) {
		info, err := os.Stat(intentPath)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(intentPath, updated, info.Mode().Perm()); err != nil {
			return nil, fmt.Errorf("failed to write intent %s: %w", intentPath, err)
		}
	}
	return changes, nil
}

type lineEdit struct {
	line    int // 0-based line index
	replace bool
	text    string
}

func pinIntentDigests(data []byte, sources []model.ResolvedCompositionSource) ([]byte, []PinChange, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("intent is empty")
	}
	sourcesNode := mappingValue(mappingValue(doc.Content[0], "compositions"), "sources")
	if sourcesNode == nil || sourcesNode.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("intent does not declare compositions.sources")
	}

	resolvedByName := make(map[string]model.ResolvedCompositionSource, len(sources))
	for _, source := range sources {
		if source.Kind == legacySourceKind {
			continue
		}
		resolvedByName[source.Name] = source
	}

	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines := strings.Split(string(data), newline)

	var edits []lineEdit
	changes := make([]PinChange, 0, len(resolvedByName))
	for _, entry := range sourcesNode.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		nameNode := mappingValue(entry, "name")
		if nameNode == nil {
			continue
		}
		resolved, ok := resolvedByName[nameNode.Value]
		if !ok {
			continue
		}
		delete(resolvedByName, nameNode.Value)
		if strings.TrimSpace(resolved.ResolvedDigest) == "" {
			return nil, nil, fmt.Errorf("composition source %s has no resolved digest", resolved.Name)
		}

		change := PinChange{Name: resolved.Name, NewDigest: resolved.ResolvedDigest}
		if digestNode := mappingValue(entry, "digest"); digestNode != nil {
			change.OldDigest = digestNode.Value
			if change.Changed() {
				edit, err := replaceScalarEdit(lines, digestNode, resolved.ResolvedDigest)
				if err != nil {
					return nil, nil, fmt.Errorf("composition source %s: %w", resolved.Name, err)
				}
				edits = append(edits, edit)
			}
		} else {
			edit, err := insertDigestEdit(lines, entry, resolved.ResolvedDigest)
			if err != nil {
				return nil, nil, fmt.Errorf("composition source %s: %w", resolved.Name, err)
			}
			edits = append(edits, edit)
		}
		changes = append(changes, change)
	}
	for name := range resolvedByName {
		return nil, nil, fmt.Errorf("resolved composition source %s is not declared in compositions.sources", name)
	}

	// Apply bottom-up so earlier line indexes stay valid after insertions.
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		if edit.replace {
			lines[edit.line] = edit.text
			continue
		}
		lines = append(lines[:edit.line], append([]string{edit.text}, lines[edit.line:]...)...)
	}
	return []byte(strings.Join(lines, newline)), changes, nil
}

// replaceScalarEdit swaps the single-line scalar at node's position for value,
// keeping anything after it on the line (such as a trailing comment).
func replaceScalarEdit(lines []string, node *yaml.Node, value string) (lineEdit, error) {
	if node.Kind != yaml.ScalarNode || node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return lineEdit{}, fmt.Errorf("digest must be a single-line scalar")
	}
	lineIdx, col := node.Line-1, node.Column-1
	if lineIdx < 0 || lineIdx >= len(lines) || col < 0 || col > len(lines[lineIdx]) {
		return lineEdit{}, fmt.Errorf("digest position out of range")
	}
	line := lines[lineIdx]
	rest := line[col:]
	var end int
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		end = closingQuote(rest, '"', true)
	case node.Style&yaml.SingleQuotedStyle != 0:
		end = closingQuote(rest, '\'', false)
	default:
		// A plain scalar's source text is its value.
		end = -1
		if strings.HasPrefix(rest, node.Value) {
			end = len(node.Value)
		}
	}
	if end < 0 {
		return lineEdit{}, fmt.Errorf("digest must be a single-line scalar")
	}
	quoted := rest[:end]
	replacement := value
	if strings.HasPrefix(quoted, "\"") || strings.HasPrefix(quoted, "'") {
		replacement = quoted[:1] + value + quoted[:1]
	}
	return lineEdit{line: lineIdx, replace: true, text: line[:col] + replacement + rest[end:]}, nil
}

// closingQuote returns the index just past the closing quote of a quoted
// scalar starting at s[0], or -1 when it does not close on this line.
func closingQuote(s string, quote byte, backslashEscapes bool) int {
	for i := 1; i < len(s); i++ {
		switch {
		case backslashEscapes && s[i] == '\\':
			i++
		case s[i] == quote:
			if !backslashEscapes && i+1 < len(s) && s[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return -1
}

// insertDigestEdit adds a `digest:` line to a block-style source mapping,
// directly after its path/ref (or name) entry and at the same indentation.
func insertDigestEdit(lines []string, entry *yaml.Node, value string) (lineEdit, error) {
	if entry.Style&yaml.FlowStyle != 0 {
		return lineEdit{}, fmt.Errorf("source uses flow style; add `digest: %s` by hand", value)
	}
	anchorKey, anchorValue := mappingEntry(entry, "ref")
	if anchorKey == nil {
		anchorKey, anchorValue = mappingEntry(entry, "path")
	}
	if anchorKey == nil {
		anchorKey, anchorValue = mappingEntry(entry, "name")
	}
	if anchorValue.Kind != yaml.ScalarNode || anchorValue.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || anchorValue.Line != anchorKey.Line {
		return lineEdit{}, fmt.Errorf("cannot place digest after a multi-line %s; add `digest: %s` by hand", anchorKey.Value, value)
	}
	lineIdx := anchorKey.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return lineEdit{}, fmt.Errorf("source position out of range")
	}
	indent := strings.Repeat(" ", anchorKey.Column-1)
	return lineEdit{line: lineIdx + 1, text: indent + "digest: " + value}, nil
}

func mappingEntry(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	_, value := mappingEntry(node, key)
	return value
}

// ReadLockFile reads the composition lock written next to the intent. It
// returns (nil, nil) when no lock has been recorded yet.
func ReadLockFile(intentPath string) (*model.CompositionLock, error) {
	data, err := os.ReadFile(LockFilePath(intentPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read composition lock: %w", err)
	}
	var lock model.CompositionLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse composition lock %s: %w", LockFilePath(intentPath), err)
	}
	return &lock, nil
}

// LockDrift describes an unpinned source whose resolution differs from the lock.
type LockDrift struct {
	Name         string
	LockedDigest string
	Digest       string
}

// CheckLockDrift compares freshly resolved sources against the recorded lock.
// Sources pinned with `digest:` in the intent are skipped (resolution already
// enforces them), as are sources the lock has no entry for. It returns the
// drifted sources and the names of unpinned sources the lock does not cover.
func CheckLockDrift(declared []model.CompositionSource, resolved []model.ResolvedCompositionSource, lock *model.CompositionLock) ([]LockDrift, []string) {
	pinned := make(map[string]bool, len(declared))
	for _, source := range declared {
		if strings.TrimSpace(source.Digest) != "" {
			pinned[source.Name] = true
		}
	}
	locked := map[string]string{}
	if lock != nil {
		for _, source := range lock.Sources {
			locked[source.Name] = source.ResolvedDigest
		}
	}

	var drift []LockDrift
	var unlocked []string
	for _, source := range resolved {
		if source.Kind == legacySourceKind || pinned[source.Name] {
			continue
		}
		digest, ok := locked[source.Name]
		if !ok {
			unlocked = append(unlocked, source.Name)
			continue
		}
		if digest != source.ResolvedDigest {
			drift = append(drift, LockDrift{Name: source.Name, LockedDigest: digest, Digest: source.ResolvedDigest})
		}
	}
	return drift, unlocked
}
