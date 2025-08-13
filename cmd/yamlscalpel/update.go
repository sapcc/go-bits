// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Unmarshals the YAML document `buf` and applies the given change requests
// to the document, returning a remarshaled form that preserves the original
// document structure as much as possible.
func update(in []byte, changeRequests map[string]any) ([]byte, error) {
	// unmarshal while preserving comments
	var document yaml.Node
	err := yaml.Unmarshal(in, &document)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal YAML document provided on stdin: %w", err)
	}

	// we can only remarshal with a consistent indent level; to minimize the
	// diff, check all indents across the document and use the one that is most
	// popular
	indentLevel := guessIndentLevel(&document)

	// traverse the document to insert the new values
	err = applyChanges(&document, "", changeRequests)
	if err != nil {
		return nil, err
	}

	// applyChanges will have removed all applied changes from `changeRequests`; complain if anything is left
	for path := range changeRequests {
		return nil, fmt.Errorf("could not apply value at path %q because this path does not exist in the input", path)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indentLevel)
	err = enc.Encode(&document)
	if err != nil {
		return nil, fmt.Errorf("could not marshal updated YAML document: %w", err)
	}
	return buf.Bytes(), nil
}

func guessIndentLevel(node *yaml.Node) int {
	votes := make(map[int]int) // key = indent level, value = number of occurrences
	checkIndentationsRecursively(node, votes)
	delete(votes, 0) // we never want no indent

	// return the indent level with the most votes
	var (
		bestIndent    = 4 // fallback value
		bestVoteCount = 0
	)
	for indent, voteCount := range votes {
		if bestVoteCount < voteCount {
			bestIndent = indent
			bestVoteCount = voteCount
		}
	}
	return bestIndent
}

func checkIndentationsRecursively(node *yaml.Node, votes map[int]int) {
	for _, subnode := range node.Content {
		// for a !!map node, its Line/Column position is where the first key is written;
		// we need to compare this to the Line/Column position of its container
		if subnode.Kind == yaml.MappingNode {
			votes[subnode.Column-node.Column]++
		}
		checkIndentationsRecursively(subnode, votes)
	}
}

func applyChanges(node *yaml.Node, path string, changeRequests map[string]any) error {
	// if this node's path matches a change request, replace the node with the new value
	// (except for document nodes: we first need to traverse into the first actual payload node below the document)
	if node.Kind != yaml.DocumentNode {
		newValue, exists := changeRequests[path]
		if exists {
			var newNode yaml.Node
			err := newNode.Encode(newValue)
			if err != nil {
				return fmt.Errorf("while trying to encode the new value for %q: %w", path, err)
			}
			*node = newNode
			delete(changeRequests, path)
		}
	}

	// traverse through child nodes
	switch node.Kind {
	case yaml.DocumentNode:
		// document nodes have exactly one child, containing the top-level data structure of the document
		if len(node.Content) != 1 {
			return fmt.Errorf("found a DocumentNode with an unexpected number of children at %q: %#v", path, *node)
		}
		return applyChanges(node.Content[0], path, changeRequests)

	case yaml.SequenceNode:
		for idx, subnode := range node.Content {
			subpath := strings.TrimPrefix(strings.Join([]string{path, strconv.Itoa(idx)}, "."), ".")
			err := applyChanges(subnode, subpath, changeRequests)
			if err != nil {
				return err
			}
		}
		return nil

	case yaml.MappingNode:
		// mapping nodes have an even number of children, because each key and each value is a child node
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("found a MappingNode with an unexpected number of children at %q: %#v", path, *node)
		}
		for idx := range len(node.Content) / 2 {
			keyNode := node.Content[2*idx]
			valueNode := node.Content[2*idx+1]

			if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
				return fmt.Errorf("found a non-string key within a MappingNode at %q: %#v", path, *keyNode)
			}
			subpath := strings.TrimPrefix(strings.Join([]string{path, keyNode.Value}, "."), ".")
			err := applyChanges(valueNode, subpath, changeRequests)
			if err != nil {
				return err
			}
		}
		return nil

	case yaml.ScalarNode:
		// nothing to traverse through
		return nil

	case yaml.AliasNode:
		// we refuse to traverse through aliases since the paths will not work like we expect
		// (if a change request refers to a path below an alias, we will just not apply this
		// change, and the operation will fail later because that change is left unapplied)
		return nil

	default:
		return fmt.Errorf("found a Node with unexpected kind = %d at %q", node.Kind, path)
	}
}
