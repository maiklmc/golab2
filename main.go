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
			break
		}
		_, err := strconv.Atoi(node.Value)
		if err == nil {
			break
		}
		if node.Tag == "!!int" {
			v.addError(node.Line, path, "must be int (invalid format despite !!int tag)")
		} else {
			v.addError(node.Line, path, "must be int")
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
	if err != nil {
		line := 0
		if fields["containerPort"] != nil {
			line = fields["containerPort"].Line
		}
		v.addError(line, "containers.ports.containerPort", "must be int")
		return
	}
	if port <= 0 || port > 65535 {
		line := 0
		if fields["containerPort"] != nil {
			line = fields["containerPort"].Line
		}
		v.addError(line, "containers.ports.containerPort", "value out of range")
	}

	v.validateNode(fields["protocol"], "containers.ports.protocol", false)
}

func (v *Validator) validateProbe(probeNode *yaml.Node) {
	fields := extractFields(probeNode)
	httpGetNode := fields["httpGet"]

	if httpGetNode == nil {
		v.addError(probeNode.Line, "containers.readinessProbe.httpGet", "is required")
		return
	}

	httpFields := extractFields(httpGetNode)
	v.validateNode(httpFields["path"], "containers.readinessProbe.httpGet.path", true)

	portNode := httpFields["port"]
	if portNode == nil {
		v.addError(httpGetNode.Line, "containers.readinessProbe.httpGet.port", "is required")
	} else {
		portStr := getScalarValue(portNode)
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			v.addError(portNode.Line, "containers.readinessProbe.httpGet.port", "value out of range")
		}
	}
}

func (v *Validator) validateResourceRequirements(node *yaml.Node) {
	fields := extractFields(node)
	v.validateNode(fields["cpu"], "resources.limits.cpu", false)
	v.validateNode(fields["memory"], "resources.limits.memory", false)
}

// Вспомогательные функции
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
	return strings.Contains(s, "_") && s == strings.ToLower(s)
}

func isValidMemoryFormat(s string) bool {
	// Пример: 1Gi, 500Mi, 2G и т.п.
	if len(s) < 2 {
		return false
	}
	suffix := s[len(s)-1]
	if suffix != 'i' {
		// Без 'i': 1G, 2M
		_, err := strconv.Atoi(s[:len(s)-1])
		return err == nil && (suffix == 'G' || suffix == 'M')
	} else {
		// С 'i': 1Gi, 500Mi
		if len(s) < 3 {
			return false
		}
		suffix2 := s[len(s)-2]
		_, err := strconv.Atoi(s[:len(s)-2])
		return err == nil && (suffix2 == 'G' || suffix2 == 'M')
	}
}

func main() {
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Println("Usage: validator <yaml-file>")
		os.Exit(1)
	}

	filename := flag.Arg(0)
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var doc yaml.Node
	err = yaml.Unmarshal(data, &doc)
	if err != nil {
		fmt.Printf("Error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	validator := NewValidator(filename)
	validator.validateNode(&doc, "", true)

	// Вывод только ошибок (без "YAML is valid")
	if len(validator.errors) > 0 {
		for _, errMsg := range validator.errors {
			fmt.Println(errMsg)
		}
		os.Exit(1)
	}
	// Если ошибок нет — молча выходим с кодом 0 (без вывода)
}
