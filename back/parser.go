package apigo

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/iancoleman/strcase"
	"github.com/zdypro888/utils"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type Packages map[string]*ast.Package

func (pkgs Packages) SearchType(mod *Mod, modf *MFile, pkgname string, name string) (*Type, error) {
	pkg, ok := pkgs[pkgname]
	if !ok {
		return nil, fmt.Errorf("not found package %s", pkgname)
	}
	for filename, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch value := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.Name == name {
							mfile := &File{mod: mod, modfile: modf, Name: filename, Pkg: pkgname, File: file}
							stype, err := mfile.ConvertType(spec.Type)
							if err != nil {
								return nil, err
							}
							if stype.Name == "struct" {
								stype.Name = name
							} else {
								stype.Aliname = name
							}
							return stype, nil
						}
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("not found type %s", name)
}

type Mod struct {
	basepath string
	fileset  *token.FileSet
	modfiles map[string]*MFile
	dirpkgs  map[string]Packages
	files    map[string]*File
}

func (mod *Mod) Init() error {
	mod.basepath = path.Join(os.Getenv("GOPATH"), "pkg/mod")
	mod.fileset = token.NewFileSet()
	mod.modfiles = make(map[string]*MFile)
	mod.dirpkgs = make(map[string]Packages)
	mod.files = make(map[string]*File)
	return nil
}

func (mod *Mod) ParseMod(fpath string) (*MFile, error) {
	if mfile, ok := mod.modfiles[fpath]; ok {
		return mfile, nil
	}
	for {
		gomod := path.Join(fpath, "go.mod")
		if utils.PathExist(gomod) {
			moddata, err := os.ReadFile(gomod)
			if err != nil {
				return nil, err
			}
			var mfile *modfile.File
			if mfile, err = modfile.Parse("go.mod", moddata, nil); err != nil {
				return nil, err
			}
			mf := &MFile{
				Path: fpath,
				File: mfile,
			}
			mod.modfiles[fpath] = mf
			return mf, nil
		} else if fpath == "/" {
			return nil, errors.New("not found go.mod")
		}
		fpath = path.Dir(fpath)
	}
}

func (mod *Mod) ParseDir(dirpath string) (Packages, error) {
	var ok bool
	var err error
	var packages Packages
	if packages, ok = mod.dirpkgs[dirpath]; !ok {
		if packages, err = parser.ParseDir(mod.fileset, dirpath, nil, parser.ParseComments); err != nil {
			return nil, err
		}
		mod.dirpkgs[dirpath] = packages
	}
	return packages, nil
}

func (mod *Mod) ParseFile(filename string) (*File, error) {
	var err error
	filedir := path.Dir(filename)
	var packages Packages
	if packages, err = mod.ParseDir(filedir); err != nil {
		return nil, err
	}
	var ok bool
	var mfile *File
	if mfile, ok = mod.files[filename]; ok {
		return mfile, nil
	}
	for pname, pkg := range packages {
		for fpath, file := range pkg.Files {
			if fpath == filename {
				var modf *MFile
				if modf, err = mod.ParseMod(filedir); err != nil {
					return nil, err
				}
				mfile = &File{mod: mod, modfile: modf, Name: filename, Pkg: pname, File: file}
				for _, decl := range file.Decls {
					switch value := decl.(type) {
					case *ast.FuncDecl:
						if value.Doc != nil {
							if method, err := mfile.parseFunc(value); err != nil {
								return nil, err
							} else if method != nil {
								mfile.Methods = append(mfile.Methods, method)
							}
						}
					}
				}
				mod.files[filename] = mfile
				return mfile, nil
			}
		}
	}
	return nil, fmt.Errorf("not found file %s", filename)
}

func (mod *Mod) SearchType(filename, impath, pkgname, name string) (*Type, error) {
	modf, err := mod.ParseMod(path.Dir(filename))
	if err != nil {
		return nil, err
	}
	for _, require := range modf.File.Require {
		if strings.HasPrefix(impath, require.Mod.Path) {
			ecsapedPath, err := module.EscapePath(require.Mod.Path)
			if err != nil {
				return nil, err
			}
			pkgpath := path.Join(mod.basepath, ecsapedPath+"@"+require.Mod.Version)
			pkgs, err := mod.ParseDir(path.Join(pkgpath, strings.TrimPrefix(impath, require.Mod.Path)))
			if err != nil {
				return nil, err
			}
			return pkgs.SearchType(mod, modf, pkgname, name)
		}
	}
	return nil, fmt.Errorf("not found type %s.%s", pkgname, name)
}

type EKind int

const (
	Array   EKind = iota
	Pointer EKind = iota
)

type Type struct {
	file    *File
	Pkg     string
	Name    string
	Aliname string
	Key     *Type
	Value   *Type
	Fields  []*Field
	Exprs   []EKind
}

func (t *Type) ReadFilds(fields *ast.FieldList) error {
	for _, field := range fields.List {
		fileType, err := t.file.ConvertType(field.Type)
		if err != nil {
			return err
		}
		if len(field.Names) == 0 {
			t.Fields = append(t.Fields, &Field{Type: fileType})
		} else {
			for _, name := range field.Names {
				if unicode.IsUpper(rune(name.Name[0])) {
					t.Fields = append(t.Fields, &Field{Name: name.Name, Type: fileType})
				}
			}
		}
	}
	return nil
}

func (t *Type) Init() error {
	switch t.Name {
	case "int", "int8", "int16", "int32":
	case "int64":
	case "uint", "uint8", "uint16", "uint32":
	case "uint64":
	case "float32":
	case "float64":
	case "string":
	case "bool":
	case "byte":
	case "any":
	case "map":
	case "error":
	case "func":
	case "struct":
	case "Time":
	default:
		if t.Pkg != "" {
			target, err := t.file.SearchType(t.Pkg, t.Name)
			if err != nil {
				return err
			}
			t.Name = target.Name
			t.Aliname = target.Aliname
			t.Fields = target.Fields
		} else {
			target, err := t.file.SearchType(t.Pkg, t.Name)
			if err != nil {
				return err
			}
			t.Pkg = t.file.Pkg
			t.Name = target.Name
			t.Aliname = target.Aliname
			t.Fields = target.Fields
		}
	}
	return nil
}

func (t *Type) ProtoType() ([]*Type, string, error) {
	var protoType string
	var exprIndex int
	var baseTypes []*Type
	switch t.Name {
	case "int", "int8", "int16", "int32":
		protoType = "int32"
	case "int64":
		protoType = "int64"
	case "uint", "uint8", "uint16", "uint32":
		protoType = "uint32"
	case "uint64":
		protoType = "uint64"
	case "float32":
		protoType = "float"
	case "float64":
		protoType = "double"
	case "string":
		protoType = "string"
	case "bool":
		protoType = "bool"
	case "byte":
		if len(t.Exprs) > 0 && t.Exprs[0] == Array {
			protoType = "bytes"
			exprIndex = 1
		} else {
			protoType = "uint32"
		}
	case "any":
		protoType = "google.protobuf.Any"
	case "map":
		keyBaseTypes, keyType, err := t.Key.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if keyBaseTypes != nil {
			baseTypes = append(baseTypes, keyBaseTypes...)
		}
		valueBaseTypes, valueType, err := t.Value.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if valueBaseTypes != nil {
			baseTypes = append(baseTypes, valueBaseTypes...)
		}
		protoType = "map<" + keyType + "," + valueType + ">"
	case "error":
		protoType = "string"
	case "func":
		protoType = "google.protobuf.Any"
	default:
		if t.Pkg == "time" && t.Name == "Time" {
			protoType = "uint64"
		} else {
			protoType = t.ProtoTypeName()
			baseTypes = append(baseTypes, t)
		}
	}
	for i := exprIndex; i < len(t.Exprs); i++ {
		if t.Exprs[i] == Array {
			protoType = "repeated " + protoType
		}
	}
	return baseTypes, protoType, nil
}

func (t *Type) ProtoTypeName() string {
	return strcase.ToCamel(t.Pkg + t.Name)
}

func (t *Type) ProtoMessage() ([]*Type, string, error) {
	var baseTypes []*Type
	writer := &strings.Builder{}
	writer.WriteString("message " + t.ProtoTypeName() + " {\n")
	for i, field := range t.Fields {
		fieldBaseTypes, fileTypeName, err := field.Type.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if field.Name == "" {
			field.Name = field.Type.Name
		}
		baseTypes = append(baseTypes, fieldBaseTypes...)
		writer.WriteString(fmt.Sprintf("  %s %s = %d;\n", fileTypeName, field.Name, i+1))
	}
	writer.WriteString("}\n")
	return baseTypes, writer.String(), nil
}

func (t *Type) ProtoMessages() (string, error) {
	btypes, message, err := t.ProtoMessage()
	if err != nil {
		return "", err
	}
	writer := &strings.Builder{}
	for _, bt := range btypes {
		if len(bt.Fields) == 0 {
			continue
		}
		bmessage, err := bt.ProtoMessages()
		if err != nil {
			return "", err
		}
		writer.WriteString(bmessage)
		writer.WriteString("\n")
	}
	writer.WriteString(message)
	return writer.String(), nil
}

func (t *Type) GoType() string {
	name := t.Name
	switch t.Name {
	case "map":
		return "map[" + t.Key.GoType() + "]" + t.Value.GoType()
	}
	if t.Pkg != "" {
		name = t.Pkg + "." + name
	}
	for _, te := range t.Exprs {
		switch te {
		case Pointer:
			name = "*" + name
		case Array:
			name = "[]" + name
		}
	}
	return name
}

func (t *Type) GoNil() string {
	if len(t.Exprs) > 0 {
		return "nil"
	}
	var zeroname string
	switch t.Name {
	case "int", "int8", "int16", "int32":
		zeroname = "0"
	case "int64":
		zeroname = "0"
	case "uint", "uint8", "uint16", "uint32":
		zeroname = "0"
	case "uint64":
		zeroname = "0"
	case "float32":
		zeroname = "0"
	case "float64":
		zeroname = "0"
	case "string":
		zeroname = "\"\""
	case "bool":
		zeroname = "false"
	case "byte":
		zeroname = "0"
	case "any":
		zeroname = "nil"
	case "map":
		zeroname = "nil"
	case "error":
		zeroname = "err"
	case "func":
		zeroname = "nil"
	default:
		zeroname = t.Name + "{}"
	}
	return zeroname
}

func (t *Type) TypeForProto() (string, error) {
	var protoType string
	var exprIndex int
	switch t.Name {
	case "int", "int8", "int16", "int32":
		protoType = "int32"
	case "int64":
		protoType = "int64"
	case "uint", "uint8", "uint16", "uint32":
		protoType = "uint32"
	case "uint64":
		protoType = "uint64"
	case "float32":
		protoType = "float32"
	case "float64":
		protoType = "float64"
	case "string":
		protoType = "string"
	case "bool":
		protoType = "bool"
	case "byte":
		if len(t.Exprs) > 0 && t.Exprs[0] == Array {
			protoType = "[]byte"
			exprIndex = 1
		} else {
			protoType = "uint32"
		}
	case "any":
		protoType = "any"
	case "map":
		keyGP, err := t.Key.TypeForProto()
		if err != nil {
			return "", err
		}
		valueGP, err := t.Value.TypeForProto()
		if err != nil {
			return "", err
		}
		protoType = "map[" + keyGP + "]" + valueGP
	case "error":
		protoType = "string"
	case "func":
		protoType = "google.protobuf.Any"
	default:
		if t.Pkg == "time" && t.Name == "Time" {
			protoType = "uint64"
		} else {
			protoType = t.ProtoTypeName()
		}
	}
	for i := exprIndex; i < len(t.Exprs); i++ {
		if t.Exprs[i] == Array {
			protoType = "[]" + protoType
		} else if t.Exprs[i] == Pointer {
			protoType = "&" + protoType
		}
	}
	return protoType, nil
}

type Field struct {
	Name string
	Type *Type
}

func (f *Field) CopyFromOrigT(parent string) (string, error) {
	writer := &strings.Builder{}
	protoGoType, err := f.Type.TypeForProto()
	if err != nil {
		return "", err
	}
	writer.WriteString(protoGoType)
	writer.WriteString("{\n")
	for _, field := range f.Type.Fields {
		fieldStr, err := field.CopyFromOrig(parent)
		if err != nil {
			return "", err
		}
		writer.WriteString(fmt.Sprintf("  %s: %s,\n", field.Name, fieldStr))
	}
	writer.WriteString("}")
	return writer.String(), nil
}

func (f *Field) CopyFromOrig(parent string) (string, error) {
	writer := &strings.Builder{}
	protoGoType, err := f.Type.TypeForProto()
	if err != nil {
		panic(err)
	}
	if len(f.Type.Fields) == 0 {
		writer.WriteString(protoGoType)
		writer.WriteString("(")
		if parent != "" {
			writer.WriteString(parent)
			writer.WriteString(".")
		}
		writer.WriteString(f.Name)
		writer.WriteString(")")
	} else {
		nextParent := f.Name
		if parent != "" {
			nextParent = parent + "." + nextParent
		}
		fieldStr, err := f.CopyFromOrigT(nextParent)
		if err != nil {
			return "", err
		}
		writer.WriteString(fieldStr)
	}
	return writer.String(), nil
}

type Methodold struct {
	Name string
	Objs []*Field
	Args []*Field
	Rets []*Field
}

type MFile struct {
	Path string
	File *modfile.File
}

type File struct {
	mod     *Mod
	modfile *MFile
	Name    string
	Pkg     string
	File    *ast.File
	Methods []*Methodold
}

func (file *File) ConvertType(typ ast.Expr) (*Type, error) {
	var tval *Type
	switch value := typ.(type) {
	case *ast.Ident:
		tval = &Type{file: file, Name: value.Name}
	case *ast.ArrayType:
		val, err := file.ConvertType(value.Elt)
		if err != nil {
			return nil, err
		}
		val.Exprs = append(val.Exprs, Array)
		tval = val
	case *ast.MapType:
		key, err := file.ConvertType(value.Key)
		if err != nil {
			return nil, err
		}
		val, err := file.ConvertType(value.Value)
		if err != nil {
			return nil, err
		}
		tval = &Type{file: file, Name: "map", Key: key, Value: val}
	case *ast.InterfaceType:
		tval = &Type{file: file, Name: "any"}
	case *ast.StructType:
		stype := &Type{file: file, Name: "struct"}
		if err := stype.ReadFilds(value.Fields); err != nil {
			return nil, err
		}
		tval = stype
	case *ast.StarExpr:
		val, err := file.ConvertType(value.X)
		if err != nil {
			return nil, err
		}
		val.Exprs = append(val.Exprs, Pointer)
		tval = val
	case *ast.SelectorExpr:
		if val, ok := value.X.(*ast.Ident); ok {
			pkgname := val.Name
			tval = &Type{file: file, Pkg: pkgname, Name: value.Sel.Name}
		} else {
			return nil, fmt.Errorf("not support type %T", value.X)
		}
	case *ast.FuncType:
		tval = &Type{file: file, Name: "func"}
	default:
		return nil, fmt.Errorf("not support type %T", typ)
	}
	if err := tval.Init(); err != nil {
		return nil, err
	}
	return tval, nil
}

func (file *File) parseFunc(fdecl *ast.FuncDecl) (*Methodold, error) {
	for _, doc := range fdecl.Doc.List {
		if strings.Contains(doc.Text, "@api") {
			method := &Methodold{Name: fdecl.Name.Name}
			if fdecl.Recv != nil {
				for _, recv := range fdecl.Recv.List {
					rt, err := file.ConvertType(recv.Type)
					if err != nil {
						return nil, err
					}
					for _, name := range recv.Names {
						method.Objs = append(method.Objs, &Field{Name: name.Name, Type: rt})
					}
				}
			}
			if fdecl.Type.Params != nil {
				for _, param := range fdecl.Type.Params.List {
					pt, err := file.ConvertType(param.Type)
					if err != nil {
						return nil, err
					}
					for _, name := range param.Names {
						method.Args = append(method.Args, &Field{Name: name.Name, Type: pt})
					}
				}
			}
			if fdecl.Type.Results != nil {
				for i, ret := range fdecl.Type.Results.List {
					rt, err := file.ConvertType(ret.Type)
					if err != nil {
						return nil, err
					}
					if len(ret.Names) == 0 {
						method.Rets = append(method.Rets, &Field{Name: fmt.Sprintf("Ret%d", i+1), Type: rt})
					} else {
						for _, name := range ret.Names {
							method.Rets = append(method.Rets, &Field{Name: name.Name, Type: rt})
						}
					}
				}
			}
			return method, nil
		}
	}
	return nil, nil
}

func (file *File) SearchType(pkgname, name string) (*Type, error) {
	if pkgname == "" || pkgname == file.Pkg {
		pkgs, err := file.mod.ParseDir(path.Dir(file.Name))
		if err != nil {
			return nil, err
		}
		return pkgs.SearchType(file.mod, file.modfile, file.Pkg, name)
	}
	for _, imp := range file.File.Imports {
		impath := strings.Trim(imp.Path.Value, "\"")
		var selname string
		if imp.Name != nil {
			selname = imp.Name.Name
		} else {
			selname = impath[strings.LastIndex(impath, "/")+1:]
		}
		if selname == pkgname {
			return file.mod.SearchType(file.Name, impath, selname, name)
		}
	}
	return nil, fmt.Errorf("not found type %s.%s", pkgname, name)
}

func (file *File) ProtoWrite() error {
	var baseTypes []*Type
	mwriter := &strings.Builder{}
	for _, f := range file.Methods {
		twriter := &strings.Builder{}
		twriter.WriteString("message ")
		twriter.WriteString(f.Name)
		twriter.WriteString("Request {\n")
		for j, param := range f.Args {
			baseType, name, err := param.Type.ProtoType()
			if err != nil {
				return err
			}
			baseTypes = append(baseTypes, baseType...)
			twriter.WriteString("\t")
			twriter.WriteString(name)
			twriter.WriteString(" ")
			twriter.WriteString(param.Name)
			twriter.WriteString(" = ")
			twriter.WriteString(strconv.Itoa(j + 1))
			twriter.WriteString(";\n")
		}
		twriter.WriteString("}\n")
		twriter.WriteString("message ")
		twriter.WriteString(f.Name)
		twriter.WriteString("Response {\n")
		for j, result := range f.Rets {
			baseType, name, err := result.Type.ProtoType()
			if err != nil {
				return err
			}
			baseTypes = append(baseTypes, baseType...)
			twriter.WriteString("\t")
			twriter.WriteString(name)
			twriter.WriteString(" ")
			twriter.WriteString(result.Name)
			twriter.WriteString(" = ")
			twriter.WriteString(strconv.Itoa(j + 1))
			twriter.WriteString(";\n")
		}
		twriter.WriteString("}\n")
		mwriter.WriteString(twriter.String())
	}
	pwriter := &strings.Builder{}
	pwriter.WriteString("syntax = \"proto3\";\n")
	pwriter.WriteString("package ")
	pwriter.WriteString(file.Pkg)
	pwriter.WriteString(";\n")
	pwriter.WriteString("option go_package = \"")
	filedir := path.Dir(file.Name)
	rfilepath := strings.TrimPrefix(filedir, file.modfile.Path)
	pwriter.WriteString(file.modfile.File.Module.Mod.Path + rfilepath)
	pwriter.WriteString("\";\n\n")
	for _, t := range baseTypes {
		bmessages, err := t.ProtoMessages()
		if err != nil {
			return err
		}
		pwriter.WriteString(bmessages)
		pwriter.WriteString("\n")
	}
	pwriter.WriteString(mwriter.String())
	filebname := strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name))
	for _, f := range file.Methods {
		pwriter.WriteString("service ")
		pwriter.WriteString(filebname)
		pwriter.WriteString(" {\n")
		pwriter.WriteString("\trpc ")
		pwriter.WriteString(f.Name)
		pwriter.WriteString(" (")
		pwriter.WriteString(f.Name)
		pwriter.WriteString("Request) returns (")
		pwriter.WriteString(f.Name)
		pwriter.WriteString("Response) {}\n")
		pwriter.WriteString("}\n")
	}

	if err := os.WriteFile(strings.Replace(file.Name, ".go", ".proto", -1), []byte(pwriter.String()), 0644); err != nil {
		return err
	}
	return nil
}

func (file *File) ClientWrite() error {
	filebname := strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name))
	mwriter := &strings.Builder{}
	mwriter.WriteString("package ")
	mwriter.WriteString(file.Pkg)
	mwriter.WriteString("\n\n")
	mwriter.WriteString("type Client struct {\n")
	mwriter.WriteString("\tClient ")
	mwriter.WriteString(strcase.ToCamel(filebname + "Client"))
	mwriter.WriteString("\n}\n")
	for _, f := range file.Methods {
		twriter := &strings.Builder{}
		twriter.WriteString("func (c *Client) ")
		twriter.WriteString(f.Name)
		twriter.WriteString("(")
		for j, param := range f.Args {
			twriter.WriteString(param.Name)
			twriter.WriteString(" ")
			twriter.WriteString(param.Type.GoType())
			if j != len(f.Args)-1 {
				twriter.WriteString(", ")
			}
		}
		twriter.WriteString(") (")
		for j, result := range f.Rets {
			// twriter.WriteString(result.Name)
			// twriter.WriteString(" ")
			twriter.WriteString(result.Type.GoType())
			if j != len(f.Rets)-1 {
				twriter.WriteString(", ")
			}
		}
		twriter.WriteString(") {\n")
		twriter.WriteString("\treq := &")
		twriter.WriteString(f.Name)
		twriter.WriteString("Request{\n")
		for _, param := range f.Args {
			twriter.WriteString("\t\t")
			twriter.WriteString(strcase.ToCamel(param.Name))
			twriter.WriteString(": ")
			cloneName, err := param.CopyFromOrig("")
			if err != nil {
				return err
			}
			twriter.WriteString(cloneName)
			twriter.WriteString(",\n")
		}
		twriter.WriteString("\t}\n")
		twriter.WriteString("\tctx, cancel := c.metaContext()\n")
		twriter.WriteString("\tdefer cancel()\n")
		twriter.WriteString("\tres, err := c.Client.")
		twriter.WriteString(f.Name)
		twriter.WriteString("(ctx, req)\n")
		twriter.WriteString("\tif err != nil {\n")
		twriter.WriteString("\t\treturn ")
		for j, result := range f.Rets {
			if j != 0 {
				twriter.WriteString(", ")
			}
			twriter.WriteString(result.Type.GoNil())
		}
		twriter.WriteString("\n\t}\n")
		twriter.WriteString("\treturn ")
		for j, result := range f.Rets {
			if j != 0 {
				twriter.WriteString(", ")
			}
			if result.Type.Name == "error" {
				twriter.WriteString("errors.New(res.")
				twriter.WriteString(result.Name)
				twriter.WriteString(")")
			} else {
				twriter.WriteString("res.")
				twriter.WriteString(result.Name)
			}
		}
		twriter.WriteString("\n}\n")
		mwriter.WriteString(twriter.String())
	}
	if err := os.WriteFile(strings.Replace(file.Name, ".go", "_client.go", -1), []byte(mwriter.String()), 0644); err != nil {
		return err
	}
	return nil
}
