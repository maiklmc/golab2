package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Validator struct {
	filename string
}

func NewValidator(filename string) *Validator {
	return &Validator{filename: filename}
}

func (v *Validator) errorf(line int, field, msg string) {
	if line > 0 {
		fmt.Printf("%s:%d %s %s\n", v.filename, line, field, msg)
	} else {
		fmt.Printf("%s %s %s\n", v.filename, field, msg)
	}
}

func (v *Validator) validateNode(node *yaml.Node, path string, line int) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		return v.validateScalar(node, path, line)
	case yaml.MappingNode:
		return v.validateMapping(node, path, line)
	case yaml.SequenceNode:
		return v.validateSequence(node, path, line)
	default:
		return true // пропускаем другие типы
	}
}

func (v *Validator) validateScalar(node *yaml.Node, path string, line int) bool {
	value := node.Value

	switch path {
	case "apiVersion":
		if value != "v1" {
			v.errorf(line, path, "has unsupported value '"+value+"'")
			return false
		}
	case "kind":
		if value != "Pod" {
			v.errorf(line, path, "has unsupported value '"+value+"'")
			return false
		}
	case "metadata.name":
		if value == "" {
			v.errorf(line, path, "is required")
			return false
		}
	case "spec.os.name":
		if value != "linux" && value != "windows" {
			v.errorf(line, path, "has unsupported value '"+value+"'")
			return false
		}
	case "containers.name":
		if value == "" {
			v.errorf(line, path, "is required")
			return false
		}
		if !isValidSnakeCase(value) {
			v.errorf(line, path, "has invalid format '"+value+"'")
			return false
		}

	case "containers.image":
		if !strings.HasPrefix(value, "registry.bigbrother.io/") || !strings.Contains(value, ":") {
			v.errorf(line, path, "has invalid format '"+value+"'")
			return false
		}
	case "containers.ports.containerPort":
		port, err := strconv.Atoi(value)
		if err != nil {
			v.errorf(line, path, "must be int")
			return false
		}
		if port <= 0 || port >= 65536 {
			v.errorf(line, path, "value out of range")
			return false
		}
	case "containers.readinessProbe.httpGet.path", "containers.livenessProbe.httpGet.path":
		if !strings.HasPrefix(value, "/") {
			v.errorf(line, path, "has invalid format '"+value+"'")
			return false
		}
	case "containers.readinessProbe.httpGet.port", "containers.livenessProbe.httpGet.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			v.errorf(line, path, "must be int")
			return false
		}
		if port <= 0 || port >= 65536 {
			v.errorf(line, path, "value out of range")
			return false
		}
	case "containers.resources.limits.memory", "containers.resources.requests.memory":
		if !isValidMemoryFormat(value) {
			v.errorf(line, path, "has invalid format '"+value+"'")
			return false
		}
	case "containers.resources.limits.cpu", "containers.resources.requests.cpu":
		_, err := strconv.Atoi(value)
		if err != nil {
			v.errorf(line, path, "must be int")
			return false
		}
	case "containers.ports.protocol":
		if value != "TCP" && value != "UDP" {
			v.errorf(line, path, "has unsupported value '"+value+"'")
			return false
		}
	}
	return true
}

func (v *Validator) validateMapping(node *yaml.Node, path string, line int) bool {
	valid := true
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		fieldPath := path
		if path != "" {
			fieldPath += "." + keyNode.Value
		} else {
			fieldPath = keyNode.Value
		}

		switch fieldPath {
		case "metadata":
			required := []string{"name"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, "metadata."+f, "is required")
				}
				valid = false
			}
		case "spec":
			required := []string{"containers"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, "spec."+f, "is required")
				}
				valid = false
			}
		case "spec.os":
			required := []string{"name"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, "spec.os."+f, "is required")
				}
				valid = false
			}
		case "containers":
			required := []string{"name", "image", "resources"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, "containers."+f, "is required")
				}
				valid = false
			}
		case "containers.ports":
			required := []string{"containerPort"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, "containers.ports."+f, "is required")
				}
				valid = false
			}
		case "containers.readinessProbe.httpGet", "containers.livenessProbe.httpGet":
			required := []string{"path", "port"}
			if !v.hasRequiredFields(valueNode, required) {
				for _, f := range required {
					v.errorf(0, fieldPath+"."+f, "is required")
				}
				valid = false
			}
		}

		if !v.validateNode(valueNode, fieldPath, valueNode.Line) {
			valid = false
		}
	}
	return valid
}

func (v *Validator) validateSequence(node *yaml.Node, path string, line int) bool {
	valid := true
	for _, item := range node.Content {
		if !v.validateNode(item, path, item.Line) {
			valid = false
		}
	}
	return valid
}

func (v *Validator) hasRequiredFields(node *yaml.Node, required []string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}

	fields := make(map[string]bool)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		fields[key] = true
	}

	for _, field := range required {
		if !fields[field] {
			return false
		}
	}
	return true
}

func isValidSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			if i == 0 {
				return false // цифра в начале запрещена
			}
			continue
		}
		if r == '_' {
			if i == 0 || i == len(s)-1 || s[i-1] == '_' {
				return false // _ в начале/конце или два подряд
			}
			continue
		}
		return false
	}
	return true
}

func isValidMemoryFormat(s string) bool {
	if len(s) < 2 {
		return false
	}
	unit := s[len(s)-2:]
	switch unit {
	case "Ki", "Mi", "Gi":
		_, err := strconv.Atoi(s[:len(s)-2])
		return err == nil
	default:
		return false
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: yamlvalid <path-to-yaml>")
		os.Exit(1)
	}

	filename := os.Args[1]
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("%s cannot read file: %s\n", filename, err)
		os.Exit(1)
	}

	var root yaml.Node
	err = yaml.Unmarshal(content, &root)
	if err != nil {
		fmt.Printf("%s cannot unmarshal YAML: %s\n", filename, err)
		os.Exit(1)
	}

	validator := NewValidator(filename)
	valid := true

	// Обходим все документы в YAML (обычно один)
	for _, doc := range root.Content {
		if doc.Kind != yaml.DocumentNode {
			continue
		}
		for _, node := range doc.Content {
			if !validator.validateNode(node, "", node.Line) {
				valid = false
			}
		}
	}

	if !valid {
		os.Exit(1)
	}

	os.Exit(0)
}
