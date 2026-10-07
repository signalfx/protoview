package schema

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type Result struct {
	Messages []protoreflect.MessageDescriptor
}

func (r Result) CommonPackagePrefix() string {
	if len(r.Messages) == 0 {
		return ""
	}
	common := strings.Split(string(r.Messages[0].ParentFile().Package()), ".")
	for _, message := range r.Messages[1:] {
		parts := strings.Split(string(message.ParentFile().Package()), ".")
		length := min(len(common), len(parts))
		i := 0
		for i < length && common[i] == parts[i] {
			i++
		}
		common = common[:i]
		if len(common) == 0 {
			return ""
		}
	}
	return strings.Join(common, ".")
}

func Load(path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	root := path
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".proto") {
			return Result{}, fmt.Errorf("schema file must have a .proto extension: %s", path)
		}
		root = filepath.Dir(path)
	}

	files := map[string]string{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(p), ".proto") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = p
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("no .proto files found at %s", path)
	}

	entries := make([]string, 0, len(files))
	if info.IsDir() {
		for name := range files {
			entries = append(entries, name)
		}
	} else {
		entries = append(entries, filepath.Base(path))
	}
	sort.Strings(entries)

	resolver := protocompile.ResolverFunc(func(name string) (protocompile.SearchResult, error) {
		if p, ok := files[filepath.ToSlash(name)]; ok {
			data, err := os.ReadFile(p)
			return protocompile.SearchResult{Source: bytes.NewReader(data)}, err
		}
		parts := strings.Split(filepath.ToSlash(name), "/")
		for i := 1; i < len(parts); i++ {
			if p, ok := files[strings.Join(parts[i:], "/")]; ok {
				data, err := os.ReadFile(p)
				return protocompile.SearchResult{Source: bytes.NewReader(data)}, err
			}
		}
		return protocompile.SearchResult{}, protoregistry.NotFound
	})

	compiler := protocompile.Compiler{Resolver: protocompile.WithStandardImports(resolver)}
	compiled, err := compiler.Compile(context.Background(), entries...)
	if err != nil {
		return Result{}, fmt.Errorf("compile schema: %w", err)
	}
	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	for _, file := range compiled {
		if file.Syntax() != protoreflect.Proto3 {
			return Result{}, fmt.Errorf("%s is not proto3", file.Path())
		}
		collectMessages(file.Messages(), byName)
	}
	result := Result{Messages: make([]protoreflect.MessageDescriptor, 0, len(byName))}
	for _, md := range byName {
		result.Messages = append(result.Messages, md)
	}
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].FullName() < result.Messages[j].FullName() })
	return result, nil
}

func collectMessages(list protoreflect.MessageDescriptors, out map[protoreflect.FullName]protoreflect.MessageDescriptor) {
	for i := 0; i < list.Len(); i++ {
		m := list.Get(i)
		if !m.IsMapEntry() {
			out[m.FullName()] = m
		}
		collectMessages(m.Messages(), out)
	}
}
