package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-yaml>\n", os.Args[0])
		os.Exit(1)
	}
	filename := os.Args[1]

	content, err := os.ReadFile(filename)
	if err != nil {
		fprintfErr(filename, 0, "cannot read file: %v", err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fprintfErr(filename, 0, "cannot unmarshal file content: %v", err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fprintfErr(filename, 0, "YAML parse error: empty document")
		os.Exit(1)
	}
	doc := root.Content[0]

	if err := validateTopLevel(filename, doc); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}

func validateTopLevel(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "document must be a mapping object")
	}

	// apiVersion
	apiNode, ok := getField(n, "apiVersion")
	if !ok {
		return failRequired(file, "apiVersion")
	}
	if apiNode.Kind != yaml.ScalarNode || apiNode.Tag != "!!str" {
		return fail(file, apiNode.Line, "apiVersion must be string")
	}
	if apiNode.Value != "v1" {
		return fail(file, apiNode.Line, "apiVersion has unsupported value '%s'", apiNode.Value)
	}

	// kind
	kindNode, ok := getField(n, "kind")
	if !ok {
		return failRequired(file, "kind")
	}
	if kindNode.Kind != yaml.ScalarNode || kindNode.Tag != "!!str" {
		return fail(file, kindNode.Line, "kind must be string")
	}
	if kindNode.Value != "Pod" {
		return fail(file, kindNode.Line, "kind has unsupported value '%s'", kindNode.Value)
	}

	// metadata
	metaNode, ok := getField(n, "metadata")
	if !ok {
		return failRequired(file, "metadata")
	}
	if err := validateMetadata(file, metaNode); err != nil {
		return err
	}

	// spec
	specNode, ok := getField(n, "spec")
	if !ok {
		return failRequired(file, "spec")
	}
	if err := validateSpec(file, specNode); err != nil {
		return err
	}

	return nil
}

func validateMetadata(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "metadata must be object")
	}

	// name (required, string)
	nameNode, ok := getField(n, "name")
	if !ok {
		return failRequired(file, "metadata.name")
	}
	if nameNode.Kind != yaml.ScalarNode || nameNode.Tag != "!!str" || strings.TrimSpace(nameNode.Value) == "" {
		return fail(file, nameNode.Line, "metadata.name must be string")
	}

	// namespace (optional string)
	if nsNode, ok := getField(n, "namespace"); ok {
		if nsNode.Kind != yaml.ScalarNode || nsNode.Tag != "!!str" {
			return fail(file, nsNode.Line, "metadata.namespace must be string")
		}
	}

	// labels (optional object of string:string)
	if labelsNode, ok := getField(n, "labels"); ok {
		if labelsNode.Kind != yaml.MappingNode {
			return fail(file, labelsNode.Line, "metadata.labels must be object")
		}
		for i := 0; i < len(labelsNode.Content); i += 2 {
			k := labelsNode.Content[i]
			v := labelsNode.Content[i+1]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return fail(file, k.Line, "metadata.labels key must be string")
			}
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
				return fail(file, v.Line, "metadata.labels value must be string")
			}
		}
	}
	return nil
}

func validateSpec(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "spec must be object")
	}

	// os (optional string: linux|windows)
	if osNode, ok := getField(n, "os"); ok {
		if osNode.Kind != yaml.ScalarNode || osNode.Tag != "!!str" {
			return fail(file, osNode.Line, "spec.os must be string")
		}
		switch strings.ToLower(osNode.Value) {
		case "linux", "windows":
			// ok
		default:
			return fail(file, osNode.Line, "spec.os has unsupported value '%s'", osNode.Value)
		}
	}

	// containers (required array)
	containersNode, ok := getField(n, "containers")
	if !ok {
		return failRequired(file, "spec.containers")
	}
	if containersNode.Kind != yaml.SequenceNode {
		return fail(file, containersNode.Line, "spec.containers must be array")
	}
	if len(containersNode.Content) == 0 {
		return fail(file, containersNode.Line, "spec.containers value out of range")
	}
	for _, c := range containersNode.Content {
		if err := validateContainer(file, c); err != nil {
			return err
		}
	}
	return nil
}

var snakeCaseRe = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

func validateContainer(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "containers item must be object")
	}

	// name (required, snake_case)
	nameNode, ok := getField(n, "name")
	if !ok {
		return failRequired(file, "containers.name")
	}
	if nameNode.Kind != yaml.ScalarNode || nameNode.Tag != "!!str" {
		return fail(file, nameNode.Line, "containers.name must be string")
	}
	if !snakeCaseRe.MatchString(nameNode.Value) {
		return fail(file, nameNode.Line, "containers.name has invalid format '%s'", nameNode.Value)
	}

	// image (required, domain + tag)
	imageNode, ok := getField(n, "image")
	if !ok {
		return failRequired(file, "containers.image")
	}
	if imageNode.Kind != yaml.ScalarNode || imageNode.Tag != "!!str" {
		return fail(file, imageNode.Line, "containers.image must be string")
	}
	if !strings.HasPrefix(imageNode.Value, "registry.bigbrother.io/") {
		return fail(file, imageNode.Line, "containers.image has invalid format '%s'", imageNode.Value)
	}
	// должен быть тег после последнего слеша
	lastSlash := strings.LastIndex(imageNode.Value, "/")
	if lastSlash == -1 || !strings.Contains(imageNode.Value[lastSlash+1:], ":") {
		return fail(file, imageNode.Line, "containers.image has invalid format '%s'", imageNode.Value)
	}

	// ports (optional array of ContainerPort)
	if portsNode, ok := getField(n, "ports"); ok {
		if portsNode.Kind != yaml.SequenceNode {
			return fail(file, portsNode.Line, "containers.ports must be array")
		}
		for _, p := range portsNode.Content {
			if err := validateContainerPort(file, p); err != nil {
				return err
			}
		}
	}

	// readinessProbe / livenessProbe (optional Probe)
	if rpNode, ok := getField(n, "readinessProbe"); ok {
		if err := validateProbe(file, rpNode, "containers.readinessProbe"); err != nil {
			return err
		}
	}
	if lpNode, ok := getField(n, "livenessProbe"); ok {
		if err := validateProbe(file, lpNode, "containers.livenessProbe"); err != nil {
			return err
		}
	}

	// resources (required)
	resNode, ok := getField(n, "resources")
	if !ok {
		return failRequired(file, "containers.resources")
	}
	if err := validateResources(file, resNode); err != nil {
		return err
	}

	return nil
}

func validateContainerPort(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "containers.ports item must be object")
	}
	cpNode, ok := getField(n, "containerPort")
	if !ok {
		return failRequired(file, "containers.ports.containerPort")
	}
	if cpNode.Kind != yaml.ScalarNode {
		return fail(file, cpNode.Line, "containerPort must be int")
	}
	port, err := toInt(cpNode)
	if err != nil {
		return fail(file, cpNode.Line, "containerPort must be int")
	}
	if port <= 0 || port >= 65536 {
		return fail(file, cpNode.Line, "containerPort value out of range")
	}

	// protocol optional: TCP|UDP
	if protoNode, ok := getField(n, "protocol"); ok {
		if protoNode.Kind != yaml.ScalarNode || protoNode.Tag != "!!str" {
			return fail(file, protoNode.Line, "protocol must be string")
		}
		switch strings.ToUpper(protoNode.Value) {
		case "TCP", "UDP":
			// ok
		default:
			return fail(file, protoNode.Line, "protocol has unsupported value '%s'", protoNode.Value)
		}
	}
	return nil
}

func validateProbe(file string, n *yaml.Node, prefix string) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "%s must be object", prefix)
	}
	hgNode, ok := getField(n, "httpGet")
	if !ok {
		return failRequired(file, prefix+".httpGet")
	}
	if hgNode.Kind != yaml.MappingNode {
		return fail(file, hgNode.Line, "%s.httpGet must be object", prefix)
	}
	// path
	pathNode, ok := getField(hgNode, "path")
	if !ok {
		return failRequired(file, prefix+".httpGet.path")
	}
	if pathNode.Kind != yaml.ScalarNode || pathNode.Tag != "!!str" {
		return fail(file, pathNode.Line, "%s.httpGet.path must be string", prefix)
	}
	if !strings.HasPrefix(pathNode.Value, "/") {
		return fail(file, pathNode.Line, "%s.httpGet.path has invalid format '%s'", prefix, pathNode.Value)
	}
	// port
	portNode, ok := getField(hgNode, "port")
	if !ok {
		return failRequired(file, prefix+".httpGet.port")
	}
	if portNode.Kind != yaml.ScalarNode {
		return fail(file, portNode.Line, "%s.httpGet.port must be int", prefix)
	}
	p, err := toInt(portNode)
	if err != nil {
		return fail(file, portNode.Line, "%s.httpGet.port must be int", prefix)
	}
	if p <= 0 || p >= 65536 {
		return fail(file, portNode.Line, "%s.httpGet.port value out of range", prefix)
	}
	return nil
}

var memRe = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

func validateResources(file string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "containers.resources must be object")
	}
	// limits (optional)
	if limitsNode, ok := getField(n, "limits"); ok {
		if err := validateRRSet(file, limitsNode, "containers.resources.limits"); err != nil {
			return err
		}
	}
	// requests (optional)
	if reqNode, ok := getField(n, "requests"); ok {
		if err := validateRRSet(file, reqNode, "containers.resources.requests"); err != nil {
			return err
		}
	}
	return nil
}

func validateRRSet(file string, n *yaml.Node, prefix string) error {
	if n.Kind != yaml.MappingNode {
		return fail(file, n.Line, "%s must be object", prefix)
	}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		v := n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
			return fail(file, k.Line, "%s key must be string", prefix)
		}
		switch k.Value {
		case "cpu":
			// integer
			if v.Kind != yaml.ScalarNode {
				return fail(file, v.Line, "%s.cpu must be int", prefix)
			}
			if _, err := toInt(v); err != nil {
				return fail(file, v.Line, "%s.cpu must be int", prefix)
			}
		case "memory":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
				return fail(file, v.Line, "%s.memory must be string", prefix)
			}
			if !memRe.MatchString(v.Value) {
				return fail(file, v.Line, "%s.memory has invalid format '%s'", prefix, v.Value)
			}
		default:
		}
	}
	return nil
}

func getField(obj *yaml.Node, key string) (*yaml.Node, bool) {
	if obj.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i < len(obj.Content); i += 2 {
		k := obj.Content[i]
		v := obj.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v, true
		}
	}
	return nil, false
}

func toInt(n *yaml.Node) (int, error) {
	return strconv.Atoi(strings.TrimSpace(n.Value))
}

func fail(file string, line int, format string, a ...any) error {
	if line > 0 {
		fprintfErr(file, line, format, a...)
	} else {
		fprintfErr(file, 0, format, a...)
	}
	return fmt.Errorf("validation failed")
}

func failRequired(file, field string) error {
	fmt.Fprintf(os.Stderr, "%s: %s is required\n", file, field)
	return fmt.Errorf("required field missing")
}

func fprintfErr(file string, line int, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if line > 0 {
		fmt.Fprintf(os.Stderr, "%s:%d %s\n", file, line, msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s\n", file, msg)
	}
}
