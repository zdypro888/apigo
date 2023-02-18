package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var errorInterface = reflect.TypeOf((*error)(nil)).Elem()

type GRPC struct {
	Package string
	service string

	messages []string
	methods  []string
	servers  []string
}

func (g *GRPC) Marshal(typ reflect.Type) {
	if typ.Kind() != reflect.Ptr {
		panic("type must be a pointer")
	}
	g.service = typ.Elem().Name()
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if method.PkgPath != "" {
			continue
		}
		g.marshalMethod(method)
	}
}

func (g *GRPC) proto3Type(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Ptr:
		return g.proto3Type(typ.Elem())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Float32:
		return "float"
	case reflect.Float64:
		return "double"
	case reflect.Bool:
		return "bool"
	case reflect.Slice, reflect.Array:
		typElem := typ.Elem()
		if typElem.Kind() == reflect.Uint8 {
			return "bytes"
		}
		return "repeated " + g.proto3Type(typElem)
	case reflect.String:
		return "string"
	case reflect.Interface:
		if typ.Implements(errorInterface) {
			return "google.rpc.Status"
		}
		return "any"
	case reflect.Struct:
		return typ.Name()
	default:
		panic("unknown type: " + typ.String())
	}
}

func (g *GRPC) marshalMethod(method reflect.Method) {
	function := method.Type

	messageWriter := &strings.Builder{}
	messageWriter.WriteString("message ")
	messageWriter.WriteString(method.Name)
	messageWriter.WriteString("Request {\n")

	paramcount := function.NumIn()
	// function.In(0) is the receiver [function.IsVariadic()]
	for i := 1; i < paramcount; i++ {
		param := function.In(i)
		paramProto3Type := g.proto3Type(param)
		messageWriter.WriteString("\t")
		messageWriter.WriteString(paramProto3Type)
		messageWriter.WriteString(" ")
		messageWriter.WriteString(param.Name())
		messageWriter.WriteString(" = ")
		messageWriter.WriteString(strconv.Itoa(i))
		messageWriter.WriteString(";\n")
	}
	messageWriter.WriteString("}\n")
	messageWriter.WriteString("message ")
	messageWriter.WriteString(method.Name)
	messageWriter.WriteString("Response {\n")
	resultcount := function.NumOut()
	for i := 0; i < resultcount; i++ {
		result := function.Out(i)
		resultProto3Type := g.proto3Type(result)
		messageWriter.WriteString("\t")
		messageWriter.WriteString(resultProto3Type)
		messageWriter.WriteString(" ")
		messageWriter.WriteString(result.Name())
		messageWriter.WriteString(" = ")
		messageWriter.WriteString(strconv.Itoa(i + 1))
		messageWriter.WriteString(";\n")
	}
	messageWriter.WriteString("}")
	g.messages = append(g.messages, messageWriter.String())

	rpcbuilder := &strings.Builder{}
	rpcbuilder.WriteString("\trpc ")
	rpcbuilder.WriteString(method.Name)
	rpcbuilder.WriteString(" (")
	rpcbuilder.WriteString(method.Name)
	rpcbuilder.WriteString("Request) returns (")
	rpcbuilder.WriteString(method.Name)
	rpcbuilder.WriteString("Response) {}")
	g.methods = append(g.methods, rpcbuilder.String())

	serverBuilder := &strings.Builder{}
	serverBuilder.WriteString("func (s *")
	serverBuilder.WriteString(g.service)
	serverBuilder.WriteString(") ")
	serverBuilder.WriteString(method.Name)
	serverBuilder.WriteString("(ctx context.Context, request *")
	if g.Package != "" {
		serverBuilder.WriteString(g.Package)
		serverBuilder.WriteString(".")
	}
	serverBuilder.WriteString(method.Name)
	serverBuilder.WriteString("Request) (*")
	if g.Package != "" {
		serverBuilder.WriteString(g.Package)
		serverBuilder.WriteString(".")
	}
	serverBuilder.WriteString(method.Name)
	serverBuilder.WriteString("Response, error) {\n")
	for i := 0; i < resultcount; i++ {
		result := function.Out(i)
		serverBuilder.WriteString(result.Name())
		if i == resultcount-1 {
			serverBuilder.WriteString(" := ")
		} else {
			serverBuilder.WriteString(", ")
		}
	}
	serverBuilder.WriteString("s.obj.")
	serverBuilder.WriteString(method.Name)
	serverBuilder.WriteString("(ctx")
	for i := 1; i < paramcount; i++ {
		param := function.In(i)
		serverBuilder.WriteString(", request.")
		serverBuilder.WriteString(param.Name())
	}
	serverBuilder.WriteString(")\n")
	serverBuilder.WriteString("return ")
	for i := 0; i < resultcount; i++ {
		result := function.Out(i)
		serverBuilder.WriteString(result.Name())
		if i != resultcount-1 {
			serverBuilder.WriteString(", ")
		}
	}
	serverBuilder.WriteString("}")
	fmt.Println(serverBuilder.String())
	g.servers = append(g.servers, serverBuilder.String())
}

func (g *GRPC) Proto3() string {
	var builder strings.Builder
	builder.WriteString("syntax = \"proto3\";\n")
	if g.Package != "" {
		builder.WriteString("package ")
		builder.WriteString(g.Package)
		builder.WriteString(";\n")
	}
	for _, message := range g.messages {
		builder.WriteString(message)
		builder.WriteString("\n")
	}
	builder.WriteString("service ")
	builder.WriteString(g.service)
	builder.WriteString(" {\n")
	for _, method := range g.methods {
		builder.WriteString(method)
		builder.WriteString("\n")
	}
	builder.WriteString("}")
	return builder.String()
}

// GenerateProtoMessageDef 根据函数定义动态生成protobuf message定义
func (g *GRPC) GenerateProto(typ reflect.Type) (string, error) {
	if typ.Kind() != reflect.Ptr {
		panic("type must be a pointer")
	}
	g.service = typ.Elem().Name()
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if method.PkgPath != "" {
			continue
		}
		g.marshalMethod(method)
	}
}
