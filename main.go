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
	errors   []string // Собираем все ошибки, а не выходим сразу
}

func NewValidator(filename string) *Validator {
	return &Validator{
		filename: filename,
		errors:   []string{},
	}
}

// errorf добавляет ошибку в список (не вызывает os.Exit)
func (v *Validator) errorf(line int, field, msg string) {
	// Берём только имя файла (без пути)
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
			v.errorf(node.Line, path, "is required")
		}
	case "spec.os.name":
		if node.Value != "linux" && node.Value != "windows" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.name":
		if !isValidSnakeCase(node.Value) {
			v.errorf(node.Line, "containers.name", "has invalid format '"+node.Value+"'")
		}
	case "containers.image":
		if !strings.HasPrefix(node.Value, "registry.bigbrother.io/") || !strings.Contains(node.Value, ":") {
			v.errorf(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "containers.ports.protocol":
		if node.Value != "TCP" && node.Value != "UDP" {
			v.errorf(node.Line, path, "has unsupported value '"+node.Value+"'")
		}
	case "containers.readinessProbe.httpGet.path", "containers.livenessProbe.httpGet.path":
		if !strings.HasPrefix(node.Value, "/") {
			v.errorf(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	case "resources.limits.memory", "resources.requests.memory":
		if !isValidMemoryFormat(node.Value) {
			v.errorf(node.Line, path, "has invalid format '"+node.Value+"'")
		}
	}
}

func (v *Validator) validateMapping(node *yaml.Node, path string, required bool) {
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		fields[keyNode.Value] = valueNode
	}

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
	case "containers.readinessProbe", "containers.livenessProbe":
		v.validateProbe(node)
	case "resources":
		v.validateNode(fields["limits"], "resources.limits", false)
		v.validateNode(fields["requests"], "resources.requests", false)
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

	v.validateNode(fields["name"], "containers.name", true)
	v.validateNode(fields["image"], "containers.image", true)
	v.validateNode(fields["ports"], "containers.ports", false)
	v.validateNode(fields["readinessProbe"], "containers.readinessProbe", false)
	v.validateNode(fields["livenessProbe"], "containers.livenessProbe", false)
	v.validateNode(fields["resources"], "containers.resources", true)
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
		v.errorf(line, "containerPort", "value out of range") // Кратчайший путь
	}
	v.validateNode(fields["protocol"], "containers.ports.protocol", false)
}

func (v *Validator) validateProbe(probeNode *yaml.Node) {
	fields := extractFields(probeNode)
	httpGetNode := fields["httpGet"]

	if httpGetNode == nil {
		v.errorf(probeNode.Line, "containers.readinessProbe.httpGet", "is required")
		return
	}

	httpFields := extractFields(httpGetNode)
	v.validateNode(httpFields["path"], "containers.readinessProbe.httpGet.path", true)

	portNode := httpFields["port"]
	if portNode == nil {
		v.errorf(httpGetNode.Line, "containers.readinessProbe.httpGet.port", "is required")
		return
	}

	portStr := portNode.Value
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port >= 65536 {
		v.errorf(portNode.Line, "containers.readinessProbe.httpGet.port", "value out of range")
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
				v.errorf(cpuNode.Line, "resources.limits.cpu", "must be int")
			} else if cpuNode.Tag != "!!int" {
				// Значение числовое, но записано как строка (например, "1")
				v.errorf(cpuNode.Line, "resources.limits.cpu", "must be int")
			}
		case "memory":
			if !isValidMemoryFormat(cpuNode.Value) {
				v.errorf(cpuNode.Line, "resources.limits.memory", "has invalid format '"+cpuNode.Value+"'")
			}
		}
	}
}

func extractFields(node *yaml.Node) map[string]*yaml.Node {
	fields := make(map[string]*yaml.Node)
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
		if i == 0 && !isLowerLetter(r) {
			return false
		}
		if r == '_' {
			if i+1 < len(s) && !isLowerLetter(rune(s[i+1])) {
				return false
			}
		} else if !isLowerLetter(r) && !isDigit(r) {
			return false
		}
	}
	return true
}

func isLowerLetter(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isValidMemoryFormat(s string) bool {
	if len(s) < 2 {
		return false
	}
	suffix := s[len(s)-2:]
	switch suffix {
	case "Ki", "Mi", "Gi":
		_, err := strconv.Atoi(s[:len(s)-2])
		return err == nil
	default:
		return false
	}
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		fmt.Println("Usage: " + os.Args[0] + " <yaml-file>")
		os.Exit(1)
	}

	filename := args[0]
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("cannot read file content: " + err.Error())
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Println("cannot unmarshal file content: " + err.Error())
		os.Exit(1)
	}

	validator := NewValidator(filename)

	// Проходим по всем документам в YAML (может быть несколько через ---)
	for _, doc := range root.Content {
		validator.validateNode(doc, "", true)
	}

	// Если есть ошибки — выводим их и завершаем с ошибкой
	if len(validator.errors) > 0 {
		for _, errMsg := range validator.errors {
			fmt.Println(errMsg)
		}
		os.Exit(1)
	}
}
