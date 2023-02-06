package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/zdypro888/utils"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func main() {
	api := NewParser()
	api.Parse("/Users/zdypro/Documents/projects/src/zdypro888/applesys/nserver/account.go")
	api.Write()
}

type Parser struct {
	modpath string
	fileSet *token.FileSet

	filename string

	packages map[string]*ast.Package
	modfile  *modfile.File

	targetPkg  string
	targetFile *ast.File
	funcs      []*APIFunction
}

type APIType struct {
	parser  *Parser
	Pkg     string
	Name    string
	Aliname string
	Exprs   []int

	MKey   *APIType
	MValue *APIType
	Fields []*APIParam
}

func (t *APIType) String() string {
	name := t.Name
	switch t.Name {
	case "map":
		return "map[" + t.MKey.String() + "]" + t.MValue.String()
	case "any":
		return "any"
	}
	if t.Pkg != "" {
		name = t.Pkg + "." + name
	}
	for _, te := range t.Exprs {
		switch te {
		case 1:
			name = "*" + name
		case 2:
			name = "[]" + name
		}
	}
	return name
}

func (t *APIType) readFilds(fields *ast.FieldList) error {
	for _, field := range fields.List {
		rt, err := t.parser.convertType(field.Type)
		if err != nil {
			return err
		}
		if len(field.Names) == 0 {
			t.Fields = append(t.Fields, &APIParam{Type: rt})
		} else {
			for _, name := range field.Names {
				t.Fields = append(t.Fields, &APIParam{Name: name.Name, Type: rt})
			}
		}
	}
	return nil
}

func (t *APIType) ProtoType() ([]*APIType, string, error) {
	var p3type string
	var p3repi int
	var err error
	var p3baseTypes []*APIType
	switch t.Name {
	case "int", "int8", "int16", "int32":
		p3type = "int32"
	case "int64":
		p3type = "int64"
	case "uint", "uint8", "uint16", "uint32":
		p3type = "uint32"
	case "uint64":
		p3type = "uint64"
	case "float32":
		p3type = "float"
	case "float64":
		p3type = "double"
	case "string":
		p3type = "string"
	case "bool":
		p3type = "bool"
	case "byte":
		if len(t.Exprs) > 0 && t.Exprs[len(t.Exprs)-1] == 2 {
			p3type = "bytes"
			p3repi = 1
		} else {
			p3type = "uint32"
		}
	case "any":
		p3type = "google.protobuf.Any"
	case "map":
		kbt, kn, err := t.MKey.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if len(kbt) > 0 {
			p3baseTypes = append(p3baseTypes, kbt...)
		}
		vbt, vn, err := t.MValue.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if len(vbt) > 0 {
			p3baseTypes = append(p3baseTypes, vbt...)
		}
		p3type = "map<" + kn + "," + vn + ">"
	case "error":
		p3type = "string"
	default:
		if t.Pkg == "time" && t.Name == "Time" {
			p3type = "uint64"
		} else {
			var real *APIType
			if t.Pkg == "" {
				real, err = t.parser.searchType(t.Name)
			} else {
				real, err = t.parser.searchImport(t.Pkg, t.Name)
			}
			if err != nil {
				return nil, "", err
			}
			if len(real.Fields) == 0 {
				return real.ProtoType()
			}
			p3baseTypes = append(p3baseTypes, real)
			p3type = real.Name
		}
	}
	for _, te := range t.Exprs {
		if te == 2 && p3repi <= 0 {
			p3type = "repeated " + p3type
		}
		p3repi--
	}
	return p3baseTypes, p3type, nil
}

func (t *APIType) ProtoMessage() ([]*APIType, string, error) {
	var baseTypes []*APIType
	writer := &strings.Builder{}
	writer.WriteString("message " + t.Name + " {\n")
	for i, field := range t.Fields {
		p3baseTypes, p3type, err := field.Type.ProtoType()
		if err != nil {
			return nil, "", err
		}
		if field.Name == "" {
			field.Name = p3type
		}
		baseTypes = append(baseTypes, p3baseTypes...)
		writer.WriteString(fmt.Sprintf("  %s %s = %d;\n", p3type, field.Name, i+1))
	}
	writer.WriteString("}\n")
	return baseTypes, writer.String(), nil
}

func (t *APIType) ProtoMessages() (string, error) {
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

type APIParam struct {
	Name string
	Type *APIType
}

type APIFunction struct {
	Name    string
	Params  []*APIParam
	Results []*APIParam
}

func NewParser() *Parser {
	return &Parser{
		modpath: path.Join(os.Getenv("GOPATH"), "pkg/mod"),
		fileSet: token.NewFileSet(),
	}
}

func (p *Parser) convertType(typ ast.Expr) (*APIType, error) {
	switch value := typ.(type) {
	case *ast.Ident:
		return &APIType{parser: p, Name: value.Name}, nil
	case *ast.ArrayType:
		val, err := p.convertType(value.Elt)
		if err != nil {
			return nil, err
		}
		val.Exprs = append(val.Exprs, 2)
		return val, nil
	case *ast.MapType:
		key, err := p.convertType(value.Key)
		if err != nil {
			return nil, err
		}
		val, err := p.convertType(value.Value)
		if err != nil {
			return nil, err
		}
		return &APIType{parser: p, Name: "map", MKey: key, MValue: val}, nil
	case *ast.InterfaceType:
		return &APIType{parser: p, Name: "any"}, nil
	case *ast.StructType:
		stype := &APIType{parser: p, Name: "struct"}
		if err := stype.readFilds(value.Fields); err != nil {
			return nil, err
		}
		return stype, nil
	case *ast.StarExpr:
		val, err := p.convertType(value.X)
		if err != nil {
			return nil, err
		}
		val.Exprs = append(val.Exprs, 1)
		return val, nil
	case *ast.SelectorExpr:
		if val, ok := value.X.(*ast.Ident); ok {
			pkgname := val.Name
			return &APIType{parser: p, Pkg: pkgname, Name: value.Sel.Name}, nil
		} else {
			return nil, fmt.Errorf("not support type %T", value.X)
		}
	default:
		return nil, fmt.Errorf("not support type %T", typ)
	}
}

func (p *Parser) searchType(typ string) (*APIType, error) {
	for _, file := range p.packages[p.targetPkg].Files {
		for _, decl := range file.Decls {
			switch value := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.Name == typ {
							np := &Parser{
								modpath:    p.modpath,
								fileSet:    p.fileSet,
								packages:   p.packages,
								modfile:    p.modfile,
								targetPkg:  p.targetPkg,
								targetFile: file,
							}
							stype, err := np.convertType(spec.Type)
							if err != nil {
								return nil, err
							}
							if stype.Name == "struct" {
								stype.Name = typ
							} else {
								stype.Aliname = typ
							}
							return stype, nil
						}
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("type not found %s", typ)
}

func (p *Parser) searchImport(pkgname string, typ string) (*APIType, error) {
	for _, imp := range p.targetFile.Imports {
		impath := strings.Trim(imp.Path.Value, "\"")
		var selname string
		if imp.Name != nil {
			selname = imp.Name.Name
		} else {
			selname = impath[strings.LastIndex(impath, "/")+1:]
		}
		if selname == pkgname {
			return p.searchPkg(selname, impath, typ)
		}
	}
	return nil, fmt.Errorf("import not found %s", pkgname)
}

func (p *Parser) searchPkg(name string, pkg string, typ string) (*APIType, error) {
	for _, require := range p.modfile.Require {
		if strings.HasPrefix(pkg, require.Mod.Path) {
			ecsapedPath, err := module.EscapePath(require.Mod.Path)
			if err != nil {
				return nil, err
			}
			pkgpath := path.Join(p.modpath, ecsapedPath+"@"+require.Mod.Version)
			pkgs, err := parser.ParseDir(p.fileSet, path.Join(pkgpath, strings.TrimPrefix(pkg, require.Mod.Path)), nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			fparser := &Parser{
				modpath:   path.Join(os.Getenv("GOPATH"), "pkg/mod"),
				fileSet:   p.fileSet,
				packages:  pkgs,
				targetPkg: name,
			}
			if err = fparser.parseMod(pkgpath); err != nil {
				return nil, err
			}
			return fparser.searchType(typ)
		}
	}
	return nil, fmt.Errorf("pkg not found %s", pkg)
}

func (p *Parser) Parse(filename string) error {
	var err error
	filedir := path.Dir(filename)
	if p.packages, err = parser.ParseDir(p.fileSet, filedir, nil, parser.ParseComments); err != nil {
		return err
	}
	if err = p.parseMod(filedir); err != nil {
		return err
	}
	p.filename = filename
	for pname, pkg := range p.packages {
		for fpath, file := range pkg.Files {
			if fpath == filename {
				p.targetPkg = pname
				p.targetFile = file
				for _, decl := range file.Decls {
					switch value := decl.(type) {
					case *ast.FuncDecl:
						if value.Doc != nil {
							if ainfo, err := p.function(value); err != nil {
								return err
							} else if ainfo != nil {
								p.funcs = append(p.funcs, ainfo)
							}
						}
					}
				}
				return nil
			}
		}
	}
	return fmt.Errorf("not found file %s", filename)
}

func (p *Parser) parseMod(fpath string) error {
	for {
		gomod := path.Join(fpath, "go.mod")
		if utils.PathExist(gomod) {
			moddata, err := os.ReadFile(gomod)
			if err != nil {
				return err
			}
			if p.modfile, err = modfile.Parse("go.mod", moddata, nil); err != nil {
				return err
			}
			return nil
		} else if fpath == "/" {
			return errors.New("not found go.mod")
		}
		fpath = path.Dir(fpath)
	}
}

func (p *Parser) function(fdecl *ast.FuncDecl) (*APIFunction, error) {
	for _, doc := range fdecl.Doc.List {
		if strings.Contains(doc.Text, "@api") {
			ainfo := &APIFunction{Name: fdecl.Name.Name}
			for _, param := range fdecl.Type.Params.List {
				pt, err := p.convertType(param.Type)
				if err != nil {
					return nil, err
				}
				for _, name := range param.Names {
					ainfo.Params = append(ainfo.Params, &APIParam{Name: name.Name, Type: pt})
				}
			}
			for i, ret := range fdecl.Type.Results.List {
				rt, err := p.convertType(ret.Type)
				if err != nil {
					return nil, err
				}
				if len(ret.Names) == 0 {
					ainfo.Results = append(ainfo.Results, &APIParam{Name: fmt.Sprintf("result_%d", i+1), Type: rt})
				} else {
					for _, name := range ret.Names {
						ainfo.Results = append(ainfo.Results, &APIParam{Name: name.Name, Type: rt})
					}
				}
			}
			return ainfo, nil
		}
	}
	return nil, nil
}

func (p *Parser) Write() error {
	var baseTypes []*APIType
	mwriter := &strings.Builder{}
	for _, f := range p.funcs {
		twriter := &strings.Builder{}
		twriter.WriteString("message ")
		twriter.WriteString(f.Name)
		twriter.WriteString("Request {\n")
		for j, param := range f.Params {
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
		for j, result := range f.Results {
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
	pwriter.WriteString(p.targetPkg)
	pwriter.WriteString(";\n")
	pwriter.WriteString("option go_package = \"")
	pwriter.WriteString(p.modfile.Module.Mod.Path)
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
	for _, f := range p.funcs {
		pwriter.WriteString("service ")
		pwriter.WriteString(f.Name)
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

	if err := os.WriteFile(strings.Replace(p.filename, ".go", ".proto", -1), []byte(pwriter.String()), 0644); err != nil {
		return err
	}
	return nil
}
