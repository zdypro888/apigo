package apigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type ExprType int

const (
	Array   ExprType = iota
	Pointer ExprType = iota
)

type NameType struct {
	Name string
	Type string
}

type FuncDecl struct {
	Name    string
	Recv    NameType
	Params  []*NameType
	Results []*NameType
}

type Service struct {
	Name    string
	Methods []*FuncDecl
}

type Parser struct {
	fileset  *token.FileSet
	Services map[string]*Service
}

func NewParser() *Parser {
	parser := &Parser{
		fileset:  token.NewFileSet(),
		Services: make(map[string]*Service),
	}
	return parser
}

func (p *Parser) ParseDir(path string) error {
	packages, err := parser.ParseDir(p.fileset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch value := decl.(type) {
				case *ast.FuncDecl:
					if value.Doc != nil {
						if err := p.parseFuncDecl(value); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (p *Parser) exprToString(typ ast.Expr) (string, error) {
	switch value := typ.(type) {
	case *ast.Ident:
		return value.Name, nil
	case *ast.ArrayType:
		val, err := p.exprToString(value.Elt)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[]%s", val), nil
	case *ast.MapType:
		key, err := p.exprToString(value.Key)
		if err != nil {
			return "", err
		}
		val, err := p.exprToString(value.Value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]%s", key, val), nil
	case *ast.InterfaceType:
		return "any", nil
	case *ast.StarExpr:
		val, err := p.exprToString(value.X)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*%s", val), nil
	case *ast.SelectorExpr:
		if val, ok := value.X.(*ast.Ident); ok {
			pkgname := val.Name
			return fmt.Sprintf("%s.%s", pkgname, value.Sel.Name), nil
		} else {
			return "", fmt.Errorf("not support selector type %T", value.X)
		}
	case *ast.FuncType:
		return "", nil
	default:
		return "", fmt.Errorf("not support type %T", typ)
	}
}

func (p *Parser) parseField(field *ast.Field) ([]*NameType, error) {
	ft, err := p.exprToString(field.Type)
	if err != nil {
		return nil, err
	}
	var names []*NameType
	if len(field.Names) == 0 {
		names = []*NameType{{Type: ft}}
	} else {
		for _, name := range field.Names {
			names = append(names, &NameType{Name: name.Name, Type: ft})
		}
	}
	return names, nil
}

func (p *Parser) parseFuncDecl(fdecl *ast.FuncDecl) error {
	for _, doc := range fdecl.Doc.List {
		if strings.Contains(doc.Text, "@api") {
			if fdecl.Recv == nil {
				return nil
			}
			if len(fdecl.Recv.List) != 1 {
				return fmt.Errorf("not support multi recv")
			}
			names, err := p.parseField(fdecl.Recv.List[0])
			if err != nil {
				return err
			}
			if len(names) != 1 {
				return fmt.Errorf("not support multi recv(names)")
			}
			method := &FuncDecl{Name: fdecl.Name.Name}
			method.Recv = *names[0]
			if fdecl.Type.Params != nil {
				for _, param := range fdecl.Type.Params.List {
					if names, err = p.parseField(param); err != nil {
						return err
					}
					method.Params = append(method.Params, names...)
				}
			}
			if fdecl.Type.Results != nil {
				for i, ret := range fdecl.Type.Results.List {
					if names, err = p.parseField(ret); err != nil {
						return err
					}
					for _, name := range names {
						if name.Name == "" {
							name.Name = fmt.Sprintf("Result%d", i)
						}
					}
					method.Results = append(method.Results, names...)
				}
			}
			serviceName := strings.TrimPrefix(method.Recv.Type, "*")
			if service, ok := p.Services[serviceName]; !ok {
				service = &Service{Name: method.Recv.Name, Methods: []*FuncDecl{method}}
				p.Services[serviceName] = service
			} else {
				service.Methods = append(service.Methods, method)
			}
			return nil
		}
	}
	return nil
}
func (p *Parser) WriteClient(path string) error {
	builder := &strings.Builder{}
	for name, service := range p.Services {
		clientName := name + "Client"
		// Generate struct type with service name and client instance
		builder.WriteString(fmt.Sprintf("type %s struct {\n", clientName))
		builder.WriteString("\tclient *apigo.Client\n")
		builder.WriteString(fmt.Sprintf("\t%s %s\n", service.Name, name))
		builder.WriteString("}\n")
		// Generate "New<service name>Client" function to get client instance
		builder.WriteString(fmt.Sprintf("\nfunc New%s(client *apigo.Client) *%s {\n", clientName, clientName))
		builder.WriteString(fmt.Sprintf("\treturn &%s{client: client}\n}\n", clientName))

		// Loop through methods to generate method code
		for _, method := range service.Methods {
			paramStrings := []string{}
			retStrings := []string{}
			for _, param := range method.Params {
				paramStrings = append(paramStrings, fmt.Sprintf("%s %s", param.Name, param.Type))
			}
			for _, ret := range method.Results {
				if ret.Type == "error" {
					continue
				}
				retStrings = append(retStrings, fmt.Sprintf("%s %s", GoCamelCase(ret.Name), ret.Type))
			}
			// Generate function signature
			funcCode := &strings.Builder{}
			funcCode.WriteString(fmt.Sprintf("func (c *%s) %s(%s) (%s) {\n", clientName, method.Name, strings.Join(paramStrings, ", "), strings.Join(retStrings, ", ")))
			// Generate request struct to hold params
			typeCode := &strings.Builder{}
			typeCode.WriteString("type Request struct {\n")
			for _, param := range method.Params {
				typeCode.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" bson:\"%s\"`\n", GoCamelCase(param.Name), param.Type, param.Name, param.Name))
			}
			typeCode.WriteString("}\n")
			funcCode.WriteString(typeCode.String())
			// Generate response struct to hold results
			responseCode := &strings.Builder{}
			responseCode.WriteString("type Response struct {\n")
			for _, ret := range method.Results {
				if ret.Type == "error" {
					continue
				}
				responseCode.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" bson:\"%s\"`\n", GoCamelCase(ret.Name), ret.Type, ret.Name, ret.Name))
			}
			responseCode.WriteString("}\n")
			funcCode.WriteString(responseCode.String())
			// Generate request object
			reqCode := &strings.Builder{}
			reqCode.WriteString("req := &Request{\n")
			for _, param := range method.Params {
				reqCode.WriteString(fmt.Sprintf("\t%s: %s,\n", GoCamelCase(param.Name), param.Name))
			}
			reqCode.WriteString("}\n")
			funcCode.WriteString(reqCode.String())
			funcCode.WriteString("}\n\n")
			builder.WriteString(funcCode.String())
		}
	}
	fmt.Println(builder.String())
	return nil
}
