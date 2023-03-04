package apigo

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
)

var errorInterface = reflect.TypeOf((*error)(nil)).Elem()
var errEmptyField = errors.New("empty field")

type FieldList ast.FieldList

func (f *FieldList) NameIndex(index int) (string, error) {
	indexOrig := index
	for _, field := range f.List {
		if len(field.Names) == 0 {
			if index == 0 {
				return fmt.Sprintf("unamed%d", indexOrig), nil
			}
			index--
		} else {
			for _, name := range field.Names {
				if index == 0 {
					return name.Name, nil
				}
				index--
			}
		}
	}
	return "", fmt.Errorf("index out of range")
}

type Type struct {
	api  *API
	typ  reflect.Type
	Path string
	Pkg  string
	Name string
}

func (t *Type) ProtoFile() string {
	if t.Pkg == t.api.Package {
		return filepath.Join(t.api.protoPath, t.Name+".proto")
	}
	return filepath.Join(t.api.protoPath, t.Pkg, t.Name+".proto")
}

func (t *Type) PkgPath() string {
	if t.api.protoMod != "" && t.Pkg != t.api.Package {
		return t.api.protoMod + "/" + t.Pkg
	}
	return ""
}

func (t *Type) WriteProto() error {
	var builder strings.Builder
	builder.WriteString("syntax = \"proto3\";\n")
	builder.WriteString("package ")
	builder.WriteString(t.Pkg)
	builder.WriteString(";\n")
	builder.WriteString("option go_package = \"")
	if t.api.protoMod != "" && t.Pkg != t.api.Package {
		builder.WriteString(t.api.protoMod)
		builder.WriteString("/")
	}
	builder.WriteString(t.Pkg)
	builder.WriteString("\";\n")
	var imports []string
	var fbuilder strings.Builder
	fbuilder.WriteString("message ")
	fbuilder.WriteString(t.Name)
	fbuilder.WriteString(" {\n")
	for i := 0; i < t.typ.NumField(); i++ {
		field := t.typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fieldType, fieldImports, err := t.api.ProtoType(field.Type)
		if err != nil {
			if err == errEmptyField {
				continue
			}
			return err
		}
		imports = append(imports, fieldImports...)
		fbuilder.WriteString("\t")
		fbuilder.WriteString(fieldType)
		fbuilder.WriteString(" ")
		fbuilder.WriteString(field.Name)
		fbuilder.WriteString(" = ")
		fbuilder.WriteString(strconv.Itoa(i + 1))
		fbuilder.WriteString(";\n")
	}
	fbuilder.WriteString("}\n")
	var err error
	for _, imp := range imports {
		if imp, err = filepath.Rel(t.api.protoPath, imp); err != nil {
			return err
		}
		builder.WriteString("import \"")
		builder.WriteString(imp)
		builder.WriteString("\";\n")
	}
	builder.WriteString(fbuilder.String())
	protofile := t.ProtoFile()
	if err := os.MkdirAll(filepath.Dir(protofile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(protofile, []byte(builder.String()), 0644); err != nil {
		return err
	}
	protorel, err := filepath.Rel(t.api.protoPath, protofile)
	if err != nil {
		return err
	}
	return t.api.protoc(protorel)
}

type Field struct {
	Name string
	Type reflect.Type
}

type Method struct {
	Name   string
	Params []*Field
	Result []*Field
}

type API struct {
	service any

	serviceValue reflect.Value
	serviceType  reflect.Type
	fileSet      *token.FileSet

	folder     string
	protoPath  string
	serverPath string
	clientPath string

	ModBase  string
	Package  string
	Typename string
	Name     string

	protoMod  string
	serverMod string
	clientMod string

	methods []*Method
	types   map[string]*Type
}

func NewAPI(service any) (*API, error) {
	api := &API{
		service: service,
		fileSet: token.NewFileSet(),
		types:   make(map[string]*Type),
	}
	if err := api.serviceInfo(); err != nil {
		return nil, err
	}
	return api, nil
}

func (api *API) Init(folder string) {
	api.folder = folder
	api.protoPath = filepath.Join(api.folder, api.Package)
	api.serverPath = filepath.Join(api.folder, "server")
	api.clientPath = filepath.Join(api.folder, "client")

	api.protoMod = filepath.Join(api.ModBase, api.Package)
	api.serverMod = filepath.Join(api.ModBase, "server")
	api.clientMod = filepath.Join(api.ModBase, "client")
}

func (api *API) serviceInfo() error {
	api.serviceValue = reflect.ValueOf(api.service)
	api.serviceType = api.serviceValue.Type()
	serviceElemType := api.serviceType
	for serviceElemType.Kind() == reflect.Ptr {
		serviceElemType = serviceElemType.Elem()
	}
	api.Package = serviceElemType.PkgPath()
	api.Typename = serviceElemType.String()
	api.Name = serviceElemType.Name()
	for i := 0; i < api.serviceType.NumMethod(); i++ {
		methodType := api.serviceType.Method(i)
		if methodType.PkgPath != "" {
			continue
		}
		if err := api.serviceMethod(methodType, api.serviceValue.Method(i)); err != nil {
			return err
		}
	}
	return nil
}

func (api *API) serviceMethod(method reflect.Method, value reflect.Value) error {
	methodPC := method.Func.Pointer()
	methodFunc := runtime.FuncForPC(methodPC)
	methodFile, methodLine := methodFunc.FileLine(methodPC)
	astFile, err := parser.ParseFile(api.fileSet, methodFile, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	var funcType *ast.FuncType
	for _, decl := range astFile.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if methodLine != api.fileSet.Position(funcDecl.Pos()).Line {
			continue
		}
		funcType = funcDecl.Type
		break
	}
	if funcType == nil {
		return fmt.Errorf("not found method: %s", method.Name)
	}
	methodType := method.Type
	me := &Method{
		Name: method.Name,
	}
	for i := 1; i < methodType.NumIn(); i++ {
		paramType := methodType.In(i)
		paramName, err := ((*FieldList)(funcType.Params)).NameIndex(i - 1)
		if err != nil {
			return err
		}
		me.Params = append(me.Params, &Field{Name: paramName, Type: paramType})
	}
	for i := 0; i < methodType.NumOut(); i++ {
		resultType := methodType.Out(i)
		var resultName string
		if resultType == errorInterface {
			resultName = "err"
		} else {
			if resultName, err = ((*FieldList)(funcType.Results)).NameIndex(i); err != nil {
				return err
			}
		}
		me.Result = append(me.Result, &Field{Name: resultName, Type: resultType})
	}
	api.methods = append(api.methods, me)
	return nil
}

func (api *API) typeImport(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Ptr:
		return api.typeImport(typ.Elem())
	case reflect.Slice:
		return api.typeImport(typ.Elem())
	case reflect.Map:
		return api.typeImport(typ.Elem())
	case reflect.Struct:
		return typ.PkgPath()
	}
	return ""
}

func (api *API) typeImports(typ reflect.Type) []string {
	switch typ.Kind() {
	case reflect.Ptr:
		return api.typeImports(typ.Elem())
	case reflect.Slice:
		return api.typeImports(typ.Elem())
	case reflect.Map:
		return api.typeImports(typ.Elem())
	case reflect.Struct:
		imports := []string{typ.PkgPath()}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			fimports := api.typeImports(field.Type)
			if len(fimports) > 0 {
				imports = append(imports, fimports...)
			}
		}
		return imports
	}
	return nil
}

func (api *API) protoc(file string) error {
	cmd := exec.Command("protoc",
		"--go_out=.",
		"--go_opt=paths=source_relative",
		"--go-grpc_out=.",
		"--go-grpc_opt=paths=source_relative",
		file)
	cmd.Dir = api.protoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (api *API) modtidy() error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = api.protoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (api *API) NilString(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Ptr:
		return "nil"
	case reflect.Slice:
		return "nil"
	case reflect.Map:
		return "nil"
	case reflect.Struct:
		return typ.String() + "{}"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "0"
	case reflect.Bool:
		return "false"
	case reflect.String:
		return `""`
	}
	return ""
}

func (api *API) ImportStruct(typ reflect.Type) (*Type, error) {
	typename := typ.String()
	switch typename {
	case "error":
		return nil, nil
	case "time.Time":
		return nil, nil
	}
	if st, ok := api.types[typename]; ok {
		return st, nil
	}
	st := &Type{api: api, typ: typ, Path: typ.PkgPath(), Name: typ.Name()}
	if pkgname := strings.Split(typename, "."); len(pkgname) == 2 {
		st.Pkg = pkgname[0]
	} else {
		st.Pkg = api.Package
	}
	return st, nil
}

// ImportType 转换结构体
func (api *API) ImportTypes(typ reflect.Type, withField bool) ([]*Type, error) {
	switch typ.Kind() {
	case reflect.Ptr:
		return api.ImportTypes(typ.Elem(), withField)
	case reflect.Slice, reflect.Array:
		typElem := typ.Elem()
		return api.ImportTypes(typElem, withField)
	case reflect.Struct:
		ctype, err := api.ImportStruct(typ)
		if err != nil {
			return nil, err
		}
		if ctype != nil {
			types := []*Type{ctype}
			if withField {
				for i := 0; i < typ.NumField(); i++ {
					field := typ.Field(i)
					fieldTyps, err := api.ImportTypes(field.Type, withField)
					if err != nil {
						return nil, err
					} else if fieldTyps != nil {
						types = append(types, fieldTyps...)
					}
				}
			}
			return types, nil
		}
	case reflect.Map:
		keyTyp, err := api.ImportTypes(typ.Key(), withField)
		if err != nil {
			return nil, err
		}
		valueTyp, err := api.ImportTypes(typ.Elem(), withField)
		if err != nil {
			return nil, err
		}
		var types []*Type
		if keyTyp != nil {
			types = append(types, keyTyp...)
		}
		if valueTyp != nil {
			types = append(types, valueTyp...)
		}
		return types, nil
	}
	return nil, nil
}

// ProtoType 获取proto3类型名称
func (api *API) ProtoType(typ reflect.Type) (string, []string, error) {
	switch typ.Kind() {
	case reflect.Ptr:
		return api.ProtoType(typ.Elem())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return "int32", nil, nil
	case reflect.Int64:
		return "int64", nil, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "uint32", nil, nil
	case reflect.Uint64:
		return "uint64", nil, nil
	case reflect.Float32:
		return "float", nil, nil
	case reflect.Float64:
		return "double", nil, nil
	case reflect.Bool:
		return "bool", nil, nil
	case reflect.Slice, reflect.Array:
		typElem := typ.Elem()
		if typElem.Kind() == reflect.Uint8 {
			return "bytes", nil, nil
		}
		elemType, elemImports, err := api.ProtoType(typElem)
		if err != nil {
			return "", nil, err
		}
		return "repeated " + elemType, elemImports, nil
	case reflect.String:
		return "string", nil, nil
	case reflect.Interface:
		if typ.Implements(errorInterface) {
			return "string", nil, nil
		}
		return "any", nil, nil
	case reflect.Struct:
		typename := typ.String()
		switch typename {
		case "error":
			return "string", nil, nil
		case "time.Time":
			return "int64", nil, nil
		}
		ctypes, err := api.ImportTypes(typ, false)
		if err != nil {
			return "", nil, err
		}
		var imports []string
		for _, ctype := range ctypes {
			if err = ctype.WriteProto(); err != nil {
				return "", nil, err
			}
			imports = append(imports, ctype.ProtoFile())
		}
		return typename, imports, nil
	case reflect.Map:
		keyType, keyImports, err := api.ProtoType(typ.Key())
		if err != nil {
			return "", nil, err
		}
		elemType, elemImports, err := api.ProtoType(typ.Elem())
		if err != nil {
			return "", nil, err
		}
		return "map<" + keyType + ", " + elemType + ">", append(keyImports, elemImports...), nil
	case reflect.Func:
		return "", nil, errEmptyField
	default:
		return "", nil, fmt.Errorf("unknown type: %s", typ.String())
	}
}

func (api *API) Proto2Go(goname string, gonew bool, protoname string, typ reflect.Type) string {
	var builder strings.Builder
	switch typ.Kind() {
	case reflect.Ptr:
		npname := "m" + GoCamelCase(goname)
		builder.WriteString(api.Proto2Go(npname, true, protoname, typ.Elem()))
		builder.WriteString("\n")
		builder.WriteString(goname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= &")
		builder.WriteString(npname)
	case reflect.Slice, reflect.Array:
		typElem := typ.Elem()
		if typElem.Kind() == reflect.Uint8 {
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(typ.String())
			builder.WriteString("(")
			builder.WriteString(protoname)
			builder.WriteString(")")
		} else {
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString("make([]")
			builder.WriteString(typElem.String())
			builder.WriteString(", len(")
			builder.WriteString(protoname)
			builder.WriteString("))\n")
			builder.WriteString("for i, v := range ")
			builder.WriteString(protoname)
			builder.WriteString(" {\n")
			builder.WriteString(api.Proto2Go("item", true, "v", typElem))
			builder.WriteString("\n")
			builder.WriteString(goname)
			builder.WriteString("[i] = item\n")
			builder.WriteString("}")
		}
	case reflect.Interface:
		if typ.Implements(errorInterface) {
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString("errors.New(")
			builder.WriteString(protoname)
			builder.WriteString(")")
		} else {
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(protoname)
		}
	case reflect.Struct:
		typename := typ.String()
		switch typename {
		case "error":
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString("errors.New(")
			builder.WriteString(protoname)
			builder.WriteString(")")
		case "time.Time":
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString("time.Unix(")
			builder.WriteString(protoname)
			builder.WriteString(", 0)")
		default:
			builder.WriteString(goname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(typ.String())
			builder.WriteString("{}\n")
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.PkgPath != "" {
					continue
				}
				if field.Type.Kind() == reflect.Func {
					continue
				}
				builder.WriteString(api.Proto2Go(goname+"."+field.Name, false, protoname+"."+GoCamelCase(field.Name), field.Type))
				builder.WriteString("\n")
			}
		}
	default:
		builder.WriteString(goname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= ")
		builder.WriteString(typ.String())
		builder.WriteString("(")
		builder.WriteString(protoname)
		builder.WriteString(")")
	}
	return builder.String()
}

func (api *API) Go2Proto(goname string, gonew bool, protoname string, typ reflect.Type) string {
	var builder strings.Builder
	switch typ.Kind() {
	case reflect.Ptr:
		npname := "m" + GoCamelCase(goname)
		builder.WriteString(npname)
		builder.WriteString(" := *")
		builder.WriteString(goname)
		builder.WriteString("\n")
		builder.WriteString(api.Go2Proto(npname, true, protoname, typ.Elem()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		builder.WriteString(protoname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= int32(")
		builder.WriteString(goname)
		builder.WriteString(")")
	case reflect.Int64:
		builder.WriteString(protoname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= int64(")
		builder.WriteString(goname)
		builder.WriteString(")")
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		builder.WriteString(protoname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= uint32(")
		builder.WriteString(goname)
		builder.WriteString(")")
	case reflect.Uint64:
		builder.WriteString(protoname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= uint64(")
		builder.WriteString(goname)
		builder.WriteString(")")
	case reflect.Slice, reflect.Array:
		typElem := typ.Elem()
		if typElem.Kind() == reflect.Uint8 {
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString("[]byte")
			builder.WriteString("(")
			builder.WriteString(goname)
			builder.WriteString(")")
		} else {
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			testring := typElem.String()
			if testring[0] == '*' {
				testring = "*pb" + testring[1:]
			} else {
				testring = "pb" + testring
			}
			builder.WriteString("make([]")
			builder.WriteString(testring)
			builder.WriteString(", len(")
			builder.WriteString(goname)
			builder.WriteString("))\n")
			builder.WriteString("for i, v := range ")
			builder.WriteString(goname)
			builder.WriteString(" {\n")
			builder.WriteString(api.Go2Proto("v", true, "item", typElem))
			builder.WriteString("\n")
			builder.WriteString(protoname)
			builder.WriteString("[i] = item\n")
			builder.WriteString("}")
		}
	case reflect.Interface:
		if typ.Implements(errorInterface) {
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(goname)
			builder.WriteString(".Error()")
		} else {
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(goname)
		}
	case reflect.Struct:
		typename := typ.String()
		switch typename {
		case "error":
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(goname)
			builder.WriteString(".Error()")
		case "time.Time":
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= ")
			builder.WriteString(goname)
			builder.WriteString(".Unix()")
		default:
			builder.WriteString(protoname)
			builder.WriteString(" ")
			if gonew {
				builder.WriteString(":")
			}
			builder.WriteString("= &")
			builder.WriteString("pb")
			builder.WriteString(typename)
			builder.WriteString("{}\n")
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.PkgPath != "" {
					continue
				}
				if field.Type.Kind() == reflect.Func {
					continue
				}
				builder.WriteString(api.Go2Proto(goname+"."+field.Name, false, protoname+"."+GoCamelCase(field.Name), field.Type))
				builder.WriteString("\n")
			}
		}
	default:
		builder.WriteString(protoname)
		builder.WriteString(" ")
		if gonew {
			builder.WriteString(":")
		}
		builder.WriteString("= ")
		builder.WriteString(typ.String())
		builder.WriteString("(")
		builder.WriteString(goname)
		builder.WriteString(")")
	}
	return builder.String()
}

func (api *API) WriteProto() error {
	var builder strings.Builder
	builder.WriteString("syntax = \"proto3\";\n")
	builder.WriteString("package ")
	builder.WriteString(api.Package)
	builder.WriteString(";\n")
	builder.WriteString("option go_package = \"")
	builder.WriteString(api.protoMod)
	builder.WriteString("\";\n")
	var imports []string
	var fbuilder strings.Builder
	for _, method := range api.methods {
		fbuilder.WriteString("message ")
		fbuilder.WriteString(method.Name)
		fbuilder.WriteString("Request {\n")
		for i, param := range method.Params {
			paramTypeName, paramImports, err := api.ProtoType(param.Type)
			if err != nil {
				if err == errEmptyField {
					continue
				}
				return err
			}
			imports = append(imports, paramImports...)
			fbuilder.WriteString(fmt.Sprintf("\t%s %s = %d;\n", paramTypeName, param.Name, i+1))
		}
		fbuilder.WriteString("}\n")
		fbuilder.WriteString("message ")
		fbuilder.WriteString(method.Name)
		fbuilder.WriteString("Response {\n")
		for i, result := range method.Result {
			if result.Type != errorInterface {
				resultTypeName, resultImports, err := api.ProtoType(result.Type)
				if err != nil {
					if err == errEmptyField {
						continue
					}
					return err
				}
				imports = append(imports, resultImports...)
				fbuilder.WriteString(fmt.Sprintf("\t%s %s = %d;\n", resultTypeName, result.Name, i+1))
			}
		}
		fbuilder.WriteString("}\n")
	}
	var err error
	for _, imp := range imports {
		if imp, err = filepath.Rel(api.protoPath, imp); err != nil {
			return err
		}
		builder.WriteString("import \"")
		builder.WriteString(imp)
		builder.WriteString("\";\n")
	}
	builder.WriteString(fbuilder.String())
	for _, method := range api.methods {
		builder.WriteString("service ")
		builder.WriteString(api.Name)
		builder.WriteString(" {\n")
		builder.WriteString("\trpc ")
		builder.WriteString(method.Name)
		builder.WriteString(" (")
		builder.WriteString(method.Name)
		builder.WriteString("Request) returns (")
		builder.WriteString(method.Name)
		builder.WriteString("Response);\n")
		builder.WriteString("}\n")
	}
	protofile := filepath.Join(api.protoPath, api.Package+".proto")
	if err := os.MkdirAll(filepath.Dir(protofile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(protofile, []byte(builder.String()), 0644); err != nil {
		return err
	}
	protorel, err := filepath.Rel(api.protoPath, protofile)
	if err != nil {
		return err
	}
	// var modbuilder strings.Builder
	// modbuilder.WriteString("module ")
	// modbuilder.WriteString(api.protoMod)
	// modbuilder.WriteString("\n")
	// modbuilder.WriteString("go 1.19\n")
	// modfile := filepath.Join(api.protoPath, "go.mod")
	// if err = os.WriteFile(modfile, []byte(modbuilder.String()), 0644); err != nil {
	// 	return err
	// }
	if err = api.protoc(protorel); err != nil {
		return err
	}
	// if err = api.modtidy(); err != nil {
	// 	return err
	// }
	return nil
}

func (api *API) WriteImports(builder *strings.Builder, typ reflect.Type) error {
	ptyps, err := api.ImportTypes(typ, true)
	if err != nil {
		return err
	}
	for _, ptyp := range ptyps {
		if pkgpath := ptyp.PkgPath(); pkgpath != "" {
			builder.WriteString("\tpb")
			builder.WriteString(ptyp.Pkg)
			builder.WriteString(" \"")
			builder.WriteString(pkgpath)
			builder.WriteString("\"\n")
		}
	}
	for _, pkgpath := range api.typeImports(typ) {
		builder.WriteString("\t\"")
		builder.WriteString(pkgpath)
		builder.WriteString("\"\n")
	}
	return nil
}

func (api *API) WriteServer() error {
	var builder strings.Builder
	builder.WriteString("package ")
	builder.WriteString("main")
	builder.WriteString("\n")

	builder.WriteString("import (\n")
	builder.WriteString("\t\"context\"\n")
	builder.WriteString("\t\"")
	builder.WriteString(api.protoMod)
	builder.WriteString("\"\n")
	for _, method := range api.methods {
		for _, param := range method.Params {
			if err := api.WriteImports(&builder, param.Type); err != nil {
				return err
			}
		}
		for _, result := range method.Result {
			if err := api.WriteImports(&builder, result.Type); err != nil {
				return err
			}
		}
	}
	builder.WriteString(")\n")

	builder.WriteString("type ")
	builder.WriteString(api.Name)
	builder.WriteString(" struct {\n")
	builder.WriteString("\t service ")
	builder.WriteString(api.Typename)
	builder.WriteString("\n")
	builder.WriteString("}\n")

	for _, method := range api.methods {
		builder.WriteString("func (s *")
		builder.WriteString(api.Name)
		builder.WriteString(") ")
		builder.WriteString(method.Name)
		builder.WriteString("(ctx context.Context, request *")
		builder.WriteString(api.Package)
		builder.WriteString(".")
		builder.WriteString(method.Name)
		builder.WriteString("Request) (*")
		builder.WriteString(api.Package)
		builder.WriteString(".")
		builder.WriteString(method.Name)
		builder.WriteString("Response, error) {\n")
		for _, param := range method.Params {
			builder.WriteString(api.Proto2Go(param.Name, true, "request."+GoCamelCase(param.Name), param.Type))
			builder.WriteString("\n")
		}
		for i, result := range method.Result {
			builder.WriteString("\t")
			builder.WriteString(result.Name)
			if i < len(method.Result)-1 {
				builder.WriteString(",")
			}
		}
		builder.WriteString(" := ")
		builder.WriteString("s.service.")
		builder.WriteString(method.Name)
		builder.WriteString("(")
		for i, param := range method.Params {
			builder.WriteString(param.Name)
			if i < len(method.Params)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString(")\n")
		if method.Result[len(method.Result)-1].Type == errorInterface {
			builder.WriteString("\tif err != nil {\n")
			builder.WriteString("\t\treturn nil, grpc.Errorf(codes.Internal, err.Error())\n")
			builder.WriteString("\t}\n")
		}
		for _, result := range method.Result {
			if result.Type != errorInterface {
				builder.WriteString(api.Go2Proto(result.Name, true, "pb"+result.Name, result.Type))
				builder.WriteString("\n")
			}
		}
		builder.WriteString("\treturn &")
		builder.WriteString(api.Package)
		builder.WriteString(".")
		builder.WriteString(method.Name)
		builder.WriteString("Response{\n")
		for _, result := range method.Result {
			if result.Type != errorInterface {
				builder.WriteString("\t\t")
				builder.WriteString(GoCamelCase(result.Name))
				builder.WriteString(": pb")
				builder.WriteString(result.Name)
				builder.WriteString(",\n")
			}
		}
		builder.WriteString("\t}, nil\n")
		builder.WriteString("}\n")
	}
	serverfile := filepath.Join(api.serverPath, "server.go")
	if err := os.MkdirAll(filepath.Dir(serverfile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(serverfile, []byte(builder.String()), 0644); err != nil {
		return err
	}
	// modbuilder := strings.Builder{}
	// modbuilder.WriteString("module ")
	// modbuilder.WriteString(api.serverMod)
	// modbuilder.WriteString("\n")
	// modbuilder.WriteString("go 1.19\n")
	// modfile := filepath.Join(api.serverPath, "go.mod")
	// if err := os.WriteFile(modfile, []byte(modbuilder.String()), 0644); err != nil {
	// 	return err
	// }
	return nil
}

func (api *API) WriteClient() error {
	var builder strings.Builder
	builder.WriteString("package ")
	builder.WriteString("main")
	builder.WriteString("\n")

	builder.WriteString("import (\n")
	builder.WriteString("\t\"context\"\n")
	builder.WriteString("\t\"")
	builder.WriteString(api.protoMod)
	builder.WriteString("\"\n")
	for _, method := range api.methods {
		for _, param := range method.Params {
			if err := api.WriteImports(&builder, param.Type); err != nil {
				return err
			}
		}
		for _, result := range method.Result {
			if err := api.WriteImports(&builder, result.Type); err != nil {
				return err
			}
		}
	}
	builder.WriteString(")\n")

	builder.WriteString("type ")
	builder.WriteString(api.Name)
	builder.WriteString(" struct {\n")
	builder.WriteString("\t client ")
	builder.WriteString(api.Package)
	builder.WriteString(".")
	builder.WriteString(api.Name)
	builder.WriteString("Client\n")
	builder.WriteString("}\n")

	builder.WriteString("func New")
	builder.WriteString(api.Name)
	builder.WriteString("(conn *grpc.ClientConn) *")
	builder.WriteString(api.Name)
	builder.WriteString(" {\n")
	builder.WriteString("\treturn &")
	builder.WriteString(api.Name)
	builder.WriteString("{\n")
	builder.WriteString("\t\tclient: ")
	builder.WriteString(api.Package)
	builder.WriteString(".New")
	builder.WriteString(api.Name)
	builder.WriteString("Client(conn),\n")
	builder.WriteString("\t}\n")
	builder.WriteString("}\n")

	for _, method := range api.methods {
		builder.WriteString("func ")
		builder.WriteString("(c *")
		builder.WriteString(api.Name)
		builder.WriteString(") ")
		builder.WriteString(method.Name)
		builder.WriteString("(")
		for i, param := range method.Params {
			builder.WriteString(param.Name)
			builder.WriteString(" ")
			builder.WriteString(param.Type.String())
			if i < len(method.Params)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString(") (")
		for i, result := range method.Result {
			builder.WriteString(result.Type.String())
			if i < len(method.Result)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString(") {\n")
		for _, param := range method.Params {
			builder.WriteString(api.Go2Proto(param.Name, true, "pb"+GoCamelCase(param.Name), param.Type))
			builder.WriteString("\n")
		}
		builder.WriteString("\tresponse, err := c.client.")
		builder.WriteString(method.Name)
		builder.WriteString("(context.Background(), &")
		builder.WriteString(api.Package)
		builder.WriteString(".")
		builder.WriteString(method.Name)
		builder.WriteString("Request{\n")
		for _, param := range method.Params {
			builder.WriteString("\t\t")
			builder.WriteString(GoCamelCase(param.Name))
			builder.WriteString(": pb")
			builder.WriteString(GoCamelCase(param.Name))
			builder.WriteString(",\n")
		}
		builder.WriteString("\t})\n")
		builder.WriteString("\tif err != nil {\n")
		builder.WriteString("\t\treturn ")
		for i, ret := range method.Result {
			if i == len(method.Result)-1 && ret.Type.Implements(errorInterface) {
				builder.WriteString("err")
			} else if nilstr := api.NilString(ret.Type); nilstr != "" {
				builder.WriteString(nilstr)
			} else {
				return fmt.Errorf("unsupported return type %s", ret.Type)
			}
			if i < len(method.Result)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString("\n")
		builder.WriteString("\t}\n")
		for _, result := range method.Result {
			if result.Type != errorInterface {
				builder.WriteString(api.Proto2Go(result.Name, true, "response."+GoCamelCase(result.Name), result.Type))
				builder.WriteString("\n")
			}
		}
		builder.WriteString("\treturn ")
		for i, result := range method.Result {
			if result.Type != errorInterface {
				builder.WriteString(result.Name)
			} else {
				builder.WriteString("nil")
			}
			if i < len(method.Result)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString("\n")
		builder.WriteString("}\n")
	}
	clientfile := filepath.Join(api.clientPath, "client.go")
	if err := os.MkdirAll(filepath.Dir(clientfile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(clientfile, []byte(builder.String()), 0644); err != nil {
		return err
	}
	// modbuilder := strings.Builder{}
	// modbuilder.WriteString("module ")
	// modbuilder.WriteString(api.clientMod)
	// modbuilder.WriteString("\n")
	// modbuilder.WriteString("go 1.19\n")
	// modfile := filepath.Join(api.clientPath, "go.mod")
	// if err := os.WriteFile(modfile, []byte(modbuilder.String()), 0644); err != nil {
	// 	return err
	// }
	return nil
}

func (api *API) WriteWork() error {
	var builder strings.Builder
	builder.WriteString("go 1.19\n")
	builder.WriteString("use (\n\n")
	builder.WriteString("\t./")
	builder.WriteString(api.Package)
	builder.WriteString("\n")
	builder.WriteString("\t")
	builder.WriteString("./server")
	builder.WriteString("\n")
	builder.WriteString("\t")
	builder.WriteString("./client")
	builder.WriteString("\n")
	builder.WriteString(")\n")
	workfile := filepath.Join(api.folder, "go.work")
	if err := os.MkdirAll(filepath.Dir(workfile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(workfile, []byte(builder.String()), 0644); err != nil {
		return err
	}
	return nil
}
