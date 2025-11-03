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
	fmt.Fprintf(os.Stderr, "%s:%d %s %s\n", v.filename, line, field, msg)
	os.Exit(1)
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
		v.validateScalar(node, path, required)
	case yaml.MappingNode:
		v.validateMapping(node, path, required)
	case yaml.SequenceNode:
		v.validateSequence(node, path, required)
	default:
		v.errorf(node.Line, path, "has unsupported node type")
	}
}

func (v *Validator) validateSequence(node *yaml.Node, path string, required bool) {
	if len(node.Content) == 0 && required {
		v.errorf(node.Line, path, "is required")
	}
}

func (v *Validator) validateScalar(node *yaml.Node, path string, required bool) {
	if node.Value == "" && required {
		v.errorf(node.Line, path, "is required")
	}
}

func (v *Validator) validateMapping(node *yaml.Node, path string, required bool) {
	if len(node.Content) == 0 && required {
		v.errorf(node.Line, path, "is required")
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		fieldPath := fmt.Sprintf("%s.%s", path, keyNode.Value)

		switch keyNode.Value {
		case "apiVersion":
			if valueNode.Value != "v1" {
				v.errorf(valueNode.Line, fieldPath, "has unsupported value '"+valueNode.Value+"'")
			}
		case "kind":
			if valueNode.Value != "Pod" {
				v.errorf(valueNode.Line, fieldPath, "has unsupported value '"+valueNode.Value+"'")
			}
		case "metadata":
			v.validateMetadata(valueNode, fieldPath)
		case "spec":
			v.validateSpec(valueNode, fieldPath)
		// другие поля верхнего уровня...
		}
	}
}

func (v *Validator) validateSpec(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	var osNode, containersNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "os":
			osNode = val
		case "containers":
			containersNode = val
		}
	}

	// os (необязательное)
	if osNode != nil {
		v.validateOS(osNode, path+".os")
	}

	// containers (обязательно)
	v.validateContainers(containersNode, path+".containers")
}

func (v *Validator) validateOS(node *yaml.Node, path string) {
	v.validateNode(node, path, false)

	var nameNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "name" {
			nameNode = node.Content[i+1]
			break
		}
	}

	if nameNode == nil {
		v.errorf(node.Line, path+".name", "is required")
	}

	if nameNode.Value != "linux" && nameNode.Value != "windows" {
		v.errorf(nameNode.Line, path+".name", "has unsupported value '"+nameNode.Value+"'")
	}
}

func (v *Validator) validateContainers(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	if node.Kind != yaml.SequenceNode {
		v.errorf(node.Line, path, "must be array")
	}

	for idx, item := range node.Content {
		containerPath := fmt.Sprintf("%s[%d]", path, idx)
		v.validateContainer(item, containerPath)
	}
}

func (v *Validator) validateContainer(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	var nameNode, imageNode, resourcesNode *yaml.Node
	var portsNode, readinessProbeNode, livenessProbeNode *yaml.Node

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "name":
			nameNode = val
		case "image":
			imageNode = val
		case "ports":
			portsNode = val
		case "readinessProbe":
			readinessProbeNode = val
		case "livenessProbe":
			livenessProbeNode = val
		case "resources":
			resourcesNode = val
		}
	}

	// name (обязательно, snake_case)
	if nameNode == nil {
		v.errorf(node.Line, path+".name", "is required")
	} else {
		if !isValidSnakeCase(nameNode.Value) {
			v.errorf(nameNode.Line, path+".name", "has invalid format '"+nameNode.Value+"'")
		}
	}

	// image (обязательно, registry.bigbrother.io + тег)
	if imageNode == nil {
		v.errorf(node.Line, path+".image", "is required")
	} else {
		if !strings.HasPrefix(imageNode.Value, "registry.bigbrother.io/") {
			v.errorf(imageNode.Line, path+".image", "must be from registry.bigbrother.io")
		}
		if !strings.Contains(imageNode.Value, ":") {
			v.errorf(imageNode.Line, path+".image", "tag is required")
		}
	}

	// resources (обязательно)
	v.validateResources(resourcesNode, path+".resources")

	// ports (необязательное)
	if portsNode != nil {
		v.validatePorts(portsNode, path+".ports")
	}

	// readinessProbe (необязательное)
	if readinessProbeNode != nil {
		v.validateProbe(readinessProbeNode, path+".readinessProbe")
	}

	// livenessProbe (необязательное)
	if livenessProbeNode != nil {
		v.validateProbe(livenessProbeNode, path+".livenessProbe")
	}
}

func (v *Validator) validatePorts(node *yaml.Node, path string) {
	v.validateNode(node, path, false)

	if node.Kind != yaml.SequenceNode {
		v.errorf(node.Line, path, "must be array")
	}

	for idx, portItem := range node.Content {
		portPath := fmt.Sprintf("%s[%d]", path, idx)
		v.validatePort(portItem, portPath)
	}
}

func (v *Validator) validatePort(node *yaml.Node, path string) {
	v.validateNode(node, path, false)


	var containerPortNode, protocolNode *yaml.Node

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "containerPort":
			containerPortNode = val
		case "protocol":
			protocolNode = val
		}
	}

	// containerPort (обязательно, 0 < x < 65536)
	if containerPortNode == nil {
		v.errorf(node.Line, path+".containerPort", "is required")
	} else {
		port, err := strconv.Atoi(containerPortNode.Value)
		if err != nil {
			v.errorf(containerPortNode.Line, path+".containerPort", "must be int")
		} else if port <= 0 || port >= 65536 {
			v.errorf(containerPortNode.Line, path+".containerPort", "value out of range")
		}
	}

	// protocol (необязательное, TCP/UDP, по умолчанию TCP)
	if protocolNode != nil {
		if protocolNode.Value != "TCP" && protocolNode.Value != "UDP" {
			v.errorf(protocolNode.Line, path+".protocol", "has unsupported value '"+protocolNode.Value+"'")
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, path string) {
	v.validateNode(node, path, false)

	var httpGetNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "httpGet" {
			httpGetNode = node.Content[i+1]
			break
		}
	}

	if httpGetNode == nil {
		v.errorf(node.Line, path+".httpGet", "is required")
	} else {
		v.validateHTTPGetAction(httpGetNode, path+".httpGet")
	}
}

func (v *Validator) validateHTTPGetAction(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	var pathNode, portNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "path":
			pathNode = val
		case "port":
			portNode = val
		}
	}

	// path (обязательно, абсолютный путь)
	if pathNode == nil {
		v.errorf(node.Line, path+".path", "is required")
	} else if !strings.HasPrefix(pathNode.Value, "/") {
		v.errorf(pathNode.Line, path+".path", "must be absolute path")
	}

	// port (обязательно, 0 < x < 65536)
	if portNode == nil {
		v.errorf(node.Line, path+".port", "is required")
	} else {
		port, err := strconv.Atoi(portNode.Value)
		if err != nil {
			v.errorf(portNode.Line, path+".port", "must be int")
		} else if port <= 0 || port >= 65536 {
			v.errorf(portNode.Line, path+".port", "value out of range")
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	var requestsNode, limitsNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "requests":
			requestsNode = val
		case "limits":
			limitsNode = val
		}
	}

	// requests (необязательное)
	if requestsNode != nil {
		v.validateResourceRequirements(requestsNode, path+".requests")
	}

	// limits (необязательное)
	if limitsNode != nil {
		v.validateResourceRequirements(limitsNode, path+".limits")
	}
}

func (v *Validator) validateResourceRequirements(node *yaml.Node, path string) {
	v.validateNode(node, path, false)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "cpu":
			// cpu должен быть целым числом
			_, err := strconv.Atoi(val.Value)
			if err != nil {
				v.errorf(val.Line, path+".cpu", "must be int")
			}
		case "memory":
			// memory должен быть в формате с единицами Gi/Mi/Ki
			if !isValidMemoryFormat(val.Value) {
				v.errorf(val.Line, path+".memory", "has invalid format '"+val.Value+"'")
			}
		}
	}
}

func (v *Validator) validateMetadata(node *yaml.Node, path string) {
	v.validateNode(node, path, true)

	var nameNode, labelsNode *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "name":
			nameNode = val
		case "labels":
			labelsNode = val
		}
	}

	if nameNode == nil {
		v.errorf(node.Line, path+".name", "is required")
	}

	if labelsNode != nil && labelsNode.Kind != yaml.MappingNode {
		v.errorf(labelsNode.Line, path+".labels", "must be object")
	}
}


// Проверка формата snake_case
func isValidSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// Проверка формата памяти (Gi, Mi, Ki)
func isValidMemoryFormat(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	unit := s[len(s)-2:]
	switch unit {
	case "Gi", "Mi", "Ki":
		_, err := strconv.Atoi(s[:len(s)-2])
		return err == nil
	default:
		return false
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]

	// Чтение файла
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read file content: %v\n", err)
		os.Exit(1)
	}

	// Десериализация YAML
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	// Проверка, что это единственный документ в файле
	if len(root.Content) != 1 {
		fmt.Fprintf(os.Stderr, "%s: invalid YAML structure\n", filename)
		os.Exit(1)
	}

	doc := root.Content[0]
	if doc.Kind != yaml.DocumentNode {
		fmt.Fprintf(os.Stderr, "%s: expected document node\n", filename)
		os.Exit(1)
	}

	// Начинаем валидацию с верхнего уровня
	validator := NewValidator(filename)
	validator.validateNode(doc.Content[0], "", true)

	// Если дошли до конца без ошибок — успех
	os.Exit(0)
}
