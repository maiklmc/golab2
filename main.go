package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Validator struct {
	filename string
	errors   []string
}

func NewValidator(filename string) *Validator {
	return &Validator{
		filename: filename,
		errors:   []string{},
	}
}

func (v *Validator) errorf(line int, field, msg string) {
	filename := filepath.Base(v.filename)
	v.errors = append(v.errors, fmt.Sprintf("%s:%d %s %s", filename, line, field, msg))
}

func (v *Validator) validateNode(node *yaml.Node, path string, required bool) {
	if node == nil {
		if required {
			v.errorf(0, path, "is required")
		}
		return
	}

	switch node.Kind {
	case yaml.ScalarNode:
		v.validateScalar(node, path)
	case yaml.MappingNode:
		v.validateMapping(node, path, required)
	case yaml.SequenceNode:
		v.validateSequence(node, path)
	}
}

func (v *Validator) validateScalar(node *yaml.Node, path string) {
	switch path {
	case "apiVersion":
		if node.Value != "v1" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "kind":
		if node.Value != "Pod" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "metadata.name":
		if node.Value == "" {
			v.errorf(node.Line, "name", "is required")
		}
	case "spec.os.name":
		if node.Value != "linux" && node.Value != "windows" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.image":
		if !strings.HasPrefix(node.Value, "registry.bigbrother.io/") || !strings.Contains(node.Value, ":") {
			v.errorf(node.Line, "image", "has invalid format '"+node.Value+"'")
		}
	case "containers.ports.protocol":
		if node.Value != "TCP" && node.Value != "UDP" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.readinessProbe.httpGet.path", "containers.livenessProbe.httpGet.path":
		if !strings.HasPrefix(node.Value, "/") {
			v.errorf(node.Line, "path", "has invalid format '"+node.Value+"'")
		}
	case "resources.limits.memory", "resources.requests.memory":
		if !isValidMemoryFormat(node.Value) {
			v.errorf(node.Line, "memory", "has invalid format '"+node.Value+"'")
		}
	}
}

func (v *Validator) validateMapping(node *yaml.Node, path string, required bool) {
	fields := extractFields(node)

	switch path {
	case "":
		v.validateNode(fields["apiVersion"], "apiVersion", true)
		v.validateNode(fields["kind"], "kind", true)
		v.validateNode(fields["metadata"], "metadata", true)
		v.validateNode(fields["spec"], "spec", true)
	case "metadata":
		v.validateNode(fields["name"], "metadata.name", true)
		v.validateNode(fields["namespace"], "namespace", false)
		v.validateNode(fields["labels"], "labels", false)
	case "spec":
		v.validateNode(fields["os"], "spec.os", false)
		v.validateNode(fields["containers"], "spec.containers", true)
	case "spec.os":
		v.validateNode(fields["name"], "spec.os.name", true)
	case "containers":
		for _, containerNode := range node.Content {
			if containerNode.Kind == yaml.MappingNode {
				v.validateContainer(containerNode)
			}
		}
	case "containers.ports":
		for _, portNode := range node.Content {
			if portNode.Kind == yaml.MappingNode {
				v.validatePort(portNode)
			}
		}
	case "containers.readinessProbe", "containers.livenessProbe":
		v.validateProbe(node)
	case "resources":
		v.validateNode(fields["limits"], "resources.limits", true)
		v.validateNode(fields["requests"], "resources.requests", true)
	case "resources.limits", "resources.requests":
		v.validateResourceRequirements(node)
	}
}

func (v *Validator) validateSequence(node *yaml.Node, path string) {
	for _, item := range node.Content {
		switch path {
		case "spec.containers":
			if item.Kind == yaml.MappingNode {
				v.validateContainer(item)
			}
		case "containers.ports":
			if item.Kind == yaml.MappingNode {
				v.validatePort(item)
			}
		}
	}
}

func (v *Validator) validateContainer(containerNode *yaml.Node) {
	fields := extractFields(containerNode)

	nameNode := fields["name"]
	if nameNode == nil || nameNode.Value == "" {
		v.errorf(
			getLineOrDefault(nameNode, containerNode.Line),
			"name",
			"is required",
		)
	} else {
		if !isValidSnakeCase(nameNode.Value) {
			v.errorf(
				nameNode.Line,
				"name",
				"has invalid format '"+nameNode.Value+"'",
			)
		}
	}

	v.validateNode(fields["image"], "image", true)
	v.validateNode(fields["ports"], "ports", false)
	v.validateNode(fields["readinessProbe"], "readinessProbe", false)
	v.validateNode(fields["livenessProbe"], "livenessProbe", false)
	v.validateNode(fields["resources"], "resources", true)
}

func (v *Validator) validatePort(portNode *yaml.Node) {
	fields := extractFields(portNode)

	portStr := getScalarValue(fields["containerPort"])
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port >= 65536 {
		line := 0
		if fields["containerPort"] != nil {
			line = fields["containerPort"].Line
		}
		v.errorf(line, "port", "value out of range")
	}
	v.validateNode(fields["protocol"], "containers.ports.protocol", false)
}

func (v *Validator) validateProbe(probeNode *yaml.Node) {
	fields := extractFields(probeNode)
	httpGetNode := fields["httpGet"]

	if httpGetNode == nil {
		v.errorf(probeNode.Line, "httpGet", "is required")
		return
	}

	httpFields := extractFields(httpGetNode)
	v.validateNode(httpFields["path"], "path", true)

	portNode := httpFields["port"]
	if portNode == nil {
		v.errorf(httpGetNode.Line, "port", "is required")
		return
	}

	portStr := portNode.Value
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port >= 65536 {
		v.errorf(portNode.Line, "port", "value out of range")
	}
}

func (v *Validator) validateResourceRequirements(node *yaml.Node) {
	fields := extractFields(node)
	for _, field := range []string{"cpu", "memory"} {
		cpuNode := fields[field]
		if cpuNode == nil {
			continue
		}
		switch field {
		case "cpu":
			_, err := strconv.Atoi(cpuNode.Value)
			if err != nil {
				v.errorf(cpuNode.Line, "cpu", "must be int")
			} else if cpuNode.Tag != "!!int" {
				v.errorf(cpuNode.Line, "cpu", "must be int")
			}
		case "memory":
			if !isValidMemoryFormat(cpuNode.Value) {
				v.errorf(cpuNode.Line, "memory", "has invalid format '"+cpuNode.Value+"'")
			}
		}
	}
}

func extractFields(node *yaml.Node) map[string]*yaml.Node {
	fields := make(map[string]*yaml.Node)
	if node.Kind != yaml.MappingNode {
		return fields
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		fields[keyNode.Value] = valueNode
	}
	return fields
}

func getScalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func isValidSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && (r < 'a' || r > 'z') {
			return false
		}
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isValidMemoryFormat(s string) bool {
	if len(s) < 2 {
		return false
	}
	suffix := s[len(s)-1]
	if suffix != 'i' {
		return false
	}
	numericPart := s[:len(s)-2]
	unit := s[len(s)-2 : len(s)]
	if unit != "Ki" && unit != "Mi" && unit != "Gi" && unit != "Ti" {
		return false
	}
	_, err := strconv.ParseFloat(numericPart, 64)
	return err == nil && len(numericPart) > 0
}

func getLineOrDefault(node *yaml.Node, defaultLine int) int {
	if node != nil && node.Line > 0 {
		return node.Line
	}
	return defaultLine
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := args[0]
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file: %v\n", err)
		os.Exit(1)
	}

	var doc yaml.Node
	err = yaml.Unmarshal(data, &doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML: %v\n", err)
		os.Exit(1)
	}

	validator := NewValidator(filename)
	validator.validateNode(&doc, "", true)

	if len(validator.errors) > 0 {
		for _, errMsg := range validator.errors {
			fmt.Println(errMsg)
		}
		os.Exit(1)
	} else {
		fmt.Println("YAML is valid")
	}
}
