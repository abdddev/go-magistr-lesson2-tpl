package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type verr struct {
	line int
	msg  string
}

type vctx struct {
	file string
	errs []verr
}

func (c *vctx) add(line int, format string, a ...any) {
	c.errs = append(c.errs, verr{line: line, msg: fmt.Sprintf(format, a...)})
}

func (c *vctx) addRequired(field string) {
	c.errs = append(c.errs, verr{line: 0, msg: fmt.Sprintf("%s is required", field)})
}

func (c *vctx) flush() int {
	if len(c.errs) == 0 {
		return 0
	}
	for _, e := range c.errs {
		if e.line > 0 {
			fmt.Fprintf(os.Stdout, "%s:%d %s\n", c.file, e.line, e.msg)
		} else {
			fmt.Fprintf(os.Stdout, "%s: %s\n", c.file, e.msg)
		}
	}
	return 1
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-yaml>\n", os.Args[0])
		os.Exit(1)
	}
	fullPath := os.Args[1]

	data, err := os.ReadFile(fullPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read file: %v\n", fullPath, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot unmarshal file content: %v\n", fullPath, err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fmt.Fprintf(os.Stderr, "%s: YAML parse error: empty document\n", fullPath)
		os.Exit(1)
	}
	doc := root.Content[0]

	ctx := &vctx{file: filepath.Base(fullPath)}
	validateTop(ctx, doc)

	exitCode := ctx.flush()

	_ = os.Stdout.Sync()

	os.Exit(exitCode)
}

func validateTop(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "document must be a mapping object")
		return
	}
	api, ok := field(n, "apiVersion")
	if !ok {
		ctx.addRequired("apiVersion")
	} else {
		if !isStr(api) {
			ctx.add(api.Line, "apiVersion must be string")
		} else if api.Value != "v1" {
			ctx.add(api.Line, "apiVersion has unsupported value '%s'", api.Value)
		}
	}

	kind, ok := field(n, "kind")
	if !ok {
		ctx.addRequired("kind")
	} else {
		if !isStr(kind) {
			ctx.add(kind.Line, "kind must be string")
		} else if kind.Value != "Pod" {
			ctx.add(kind.Line, "kind has unsupported value '%s'", kind.Value)
		}
	}

	spec, ok := field(n, "spec")
	if !ok {
		ctx.addRequired("spec")
	} else {
		validateSpec(ctx, spec)
	}

	meta, ok := field(n, "metadata")
	if !ok {
		ctx.addRequired("metadata")
	} else {
		validateMetadata(ctx, meta)
	}

	if spec != nil {
		validateSpecContainers(ctx, spec)
	}
}

func validateMetadata(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "metadata must be object")
		return
	}
	name, ok := field(n, "name")
	if !ok {
		ctx.addRequired("name")
	} else {
		if !isStr(name) {
			ctx.add(name.Line, "name must be string")
		} else if strings.TrimSpace(name.Value) == "" {
			ctx.add(name.Line, "name is required")
		}
	}

	if ns, ok := field(n, "namespace"); ok {
		if !isStr(ns) {
			ctx.add(ns.Line, "namespace must be string")
		}
	}

	if labels, ok := field(n, "labels"); ok {
		if labels.Kind != yaml.MappingNode {
			ctx.add(labels.Line, "labels must be object")
		} else {
			for i := 0; i < len(labels.Content); i += 2 {
				k := labels.Content[i]
				v := labels.Content[i+1]
				if !isStr(k) {
					ctx.add(k.Line, "labels key must be string")
				}
				if !isStr(v) {
					ctx.add(v.Line, "labels value must be string")
				}
			}
		}
	}
}

func validateSpec(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "spec must be object")
		return
	}
	if osN, ok := field(n, "os"); ok {
		if !isStr(osN) {
			ctx.add(osN.Line, "os must be string")
		} else {
			switch strings.ToLower(osN.Value) {
			case "linux", "windows":
			default:
				ctx.add(osN.Line, "os has unsupported value '%s'", osN.Value)
			}
		}
	}
}

func validateSpecContainers(ctx *vctx, n *yaml.Node) {
	containers, ok := field(n, "containers")
	if !ok {
		ctx.addRequired("containers")
		return
	}
	if containers.Kind != yaml.SequenceNode {
		ctx.add(containers.Line, "containers must be array")
		return
	}
	if len(containers.Content) == 0 {
		ctx.add(containers.Line, "containers value out of range")
		return
	}
	for _, c := range containers.Content {
		validateContainer(ctx, c)
	}
}

var snake = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
var memRe = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

func validateContainer(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "containers item must be object")
		return
	}
	// name (required, snake_case)
	if name, ok := field(n, "name"); !ok {
		ctx.addRequired("name")
	} else {
		if !isStr(name) {
			ctx.add(name.Line, "name must be string")
		} else if strings.TrimSpace(name.Value) == "" {
			ctx.add(name.Line, "name is required")
		} else if !snake.MatchString(name.Value) {
			ctx.add(name.Line, "name has invalid format '%s'", name.Value)
		}
	}

	if img, ok := field(n, "image"); !ok {
		ctx.addRequired("image")
	} else {
		if !isStr(img) {
			ctx.add(img.Line, "image must be string")
		} else if !strings.HasPrefix(img.Value, "registry.bigbrother.io/") {
			ctx.add(img.Line, "image has invalid format '%s'", img.Value)
		} else {
			seg := img.Value[strings.LastIndex(img.Value, "/")+1:]
			if !strings.Contains(seg, ":") {
				ctx.add(img.Line, "image has invalid format '%s'", img.Value)
			}
		}
	}

	if ports, ok := field(n, "ports"); ok {
		if ports.Kind != yaml.SequenceNode {
			ctx.add(ports.Line, "ports must be array")
		} else {
			for _, p := range ports.Content {
				validateContainerPort(ctx, p)
			}
		}
	}

	if rp, ok := field(n, "readinessProbe"); ok {
		validateProbe(ctx, rp, "readinessProbe")
	}
	if lp, ok := field(n, "livenessProbe"); ok {
		validateProbe(ctx, lp, "livenessProbe")
	}

	if res, ok := field(n, "resources"); !ok {
		ctx.addRequired("resources")
	} else {
		validateResources(ctx, res)
	}
}

func validateContainerPort(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "ports item must be object")
		return
	}
	cp, ok := field(n, "containerPort")
	if !ok {
		ctx.addRequired("containerPort")
		return
	}
	port, okInt, line := asInt(cp)
	if !okInt {
		ctx.add(line, "containerPort must be int")
	} else if port <= 0 || port >= 65536 {
		ctx.add(line, "containerPort value out of range")
	}

	if proto, ok := field(n, "protocol"); ok {
		if !isStr(proto) {
			ctx.add(proto.Line, "protocol must be string")
		} else {
			switch strings.ToUpper(proto.Value) {
			case "TCP", "UDP":
			default:
				ctx.add(proto.Line, "protocol has unsupported value '%s'", proto.Value)
			}
		}
	}
}

func validateProbe(ctx *vctx, n *yaml.Node, short string) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "%s must be object", short)
		return
	}
	hg, ok := field(n, "httpGet")
	if !ok {
		ctx.addRequired("httpGet")
		return
	}
	if hg.Kind != yaml.MappingNode {
		ctx.add(hg.Line, "httpGet must be object")
		return
	}
	path, ok := field(hg, "path")
	if !ok {
		ctx.addRequired("path")
	} else {
		if !isStr(path) {
			ctx.add(path.Line, "path must be string")
		} else if !strings.HasPrefix(path.Value, "/") {
			ctx.add(path.Line, "path has invalid format '%s'", path.Value)
		}
	}
	prt, ok := field(hg, "port")
	if !ok {
		ctx.addRequired("port")
	} else {
		if v, okInt, line := asInt(prt); !okInt {
			ctx.add(line, "port must be int")
		} else if v <= 0 || v >= 65536 {
			ctx.add(prt.Line, "port value out of range")
		}
	}
}

func validateResources(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "resources must be object")
		return
	}
	if lim, ok := field(n, "limits"); ok {
		validateRRSet(ctx, lim)
	}
	if req, ok := field(n, "requests"); ok {
		validateRRSet(ctx, req)
	}
}

func validateRRSet(ctx *vctx, n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		ctx.add(n.Line, "resources entry must be object")
		return
	}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		v := n.Content[i+1]
		switch k.Value {
		case "cpu":
			if !isInt(v) {
				ctx.add(v.Line, "cpu must be int")
			}
		case "memory":
			if !isStr(v) {
				ctx.add(v.Line, "memory must be string")
			} else if !memRe.MatchString(v.Value) {
				ctx.add(v.Line, "memory has invalid format '%s'", v.Value)
			}
		default:
		}
	}
}

func field(obj *yaml.Node, key string) (*yaml.Node, bool) {
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

func isStr(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!str" }

func isInt(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!int" }

func asInt(n *yaml.Node) (int, bool, int) {
	if n.Kind != yaml.ScalarNode {
		return 0, false, n.Line
	}
	i, err := strconv.Atoi(strings.TrimSpace(n.Value))
	if err != nil {
		return 0, false, n.Line
	}
	return i, true, n.Line
}
