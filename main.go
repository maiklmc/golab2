package main

import (
	"flag"
	"fmt"
	"os"
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

func (v *Validator) addError(line int, field, msg string) {
	v.errors = append(v.errors, fmt.Sprintf("%s:%d %s %s", v.filename, line, field, msg))
}

func (v *Validator) validateNode(node *yaml.Node, path string, required bool) {
	if node == nil {
		if required {
			v.addError(0, path, "is required")
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
			v.addError(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "kind":
		if node.Value != "Pod" {
			v.addError(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "metadata.name":
		if node.Value == "" {
			v.addError(node.Line, path, "is required")
		}
	case "spec.os.name":
		if node.Value != "linux" && node.Value != "windows" {
			v.addError(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.name":
		if node.Value == "" {
			v.addError(node.Line, path, "is required")
		} else if !isValidSnakeCase(node.Value) {
			v.addError(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "containers.image":
		if !strings.HasPrefix(node.Value, "registry.bigbrother.io/") || !strings.Contains(node.Value, ":") {
			v.addError(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "containers.ports.protocol":
		if node.Value != "TCP" && node.Value != "UDP" {
			v.addError(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.readinessProbe.httpGet.path", "containers.livenessProbe.httpGet.path":
		if !strings.HasPrefix(node.Value, "/") {
			v.addError(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "resources.limits.memory", "resources.requests.memory":
		if !isValidMemoryFormat(node.Value) {
			v.addError(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "resources.limits.cpu", "resources.requests.cpu":
		if node.Value == "" {
			v.addError(node.Line, path, "must be int (empty value)")
			return
		}
		cleanValue := strings.Trim(node.Value, `"'`)
		_, err := strconv.Atoi(cleanValue)
		if err != nil {
			v.addError(node.Line, path, "must be int")
		}
	case "containers.ports.containerPort":
		port, err := strconv.Atoi(node.Value)
		if err != nil || port <= 0 || port > 65535 {
			v.addError(node.Line, path, "value out of range")
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
		v.validateNode(fields["namespace"], "metadata.namespace", false)
		v.validateNode(fields["labels"], "metadata.labels", false)
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

	v.validateNode(fields["name"], "containers.name", true)
	v.validateNode(fields["image"], "containers.image", true)
	v.validateNode(fields["ports"], "containers.ports", false)
	v.validateNode(fields["readinessProbe"], "containers.readinessProbe", false)
	v.validateNode(fields["livenessProbe"], "containers.livenessProbe", false)
	v.validateNode(fields["resources"], "containers.resources", true)
}

func (v *Validator) validatePort(portNode *yaml.Node) {
	fields := extractFields(portNode)

	v.validateNode(fields["containerPort"], "containers.ports.containerPort", true)
	v.validateNode(fields["protocol"], "containers.ports.protocol", false)
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

func isValidSnakeCase(name string) bool {
	if name == "" {
		return false
	}
	for i, char := range name {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '_' && i > 0 && i < len(name)-1 {
			continue
		}
		return false
	}
	return true
}

func isValidMemoryFormat(value string) bool {
	if value == "" {
		return false
	}
	hasDigit := false
	for _, char := range value {
		if char >= '0' && char <= '9' {
			hasDigit = true
		} else if char == 'M' || char == 'G' || char == 'K' {
			// Суффикс
		} else if char == 'i' {
			// Часть суффикса (Mi, Gi)
		} else {
			return false // Недопустимый символ
		}
	}
	return hasDigit && (strings.HasSuffix(value, "Mi") || strings.HasSuffix(value, "Gi") || strings.HasSuffix(value, "Ki"))
}

func main() {
	filename := flag.String("file", "", "YAML file to validate")
	flag.Parse()

	// Если файл не указан — молча выходим с ошибкой (без вывода Usage)
	if *filename == "" {
		os.Exit(1)
	}

	data, err := os.ReadFile(*filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var yamlNode yaml.Node
	err = yaml.Unmarshal(data, &yamlNode)
	if err != nil {
		fmt.Printf("Error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	validator := NewValidator(*filename)
	validator.validateNode(&yamlNode, "", true)

	// Выводим только ошибки валидации (как ожидают тесты)
	for _, errMsg := range validator.errors {
		fmt.Println(errMsg)
	}

	// Если есть ошибки — exit 1, иначе 0
	if len(validator.errors) > 0 {
		os.Exit(1)
	}
}
