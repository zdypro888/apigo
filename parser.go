package apigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	Decl    *ast.FuncDecl
	Name    string
	Recv    NameType
	Params  []*NameType
	Results []*NameType

	LastResultIndex int
	HasNormalResult bool
	LastResultError bool
}

func (method *FuncDecl) Init() {
	method.LastResultIndex = len(method.Results) - 1
	method.HasNormalResult = method.LastResultIndex >= 0 && method.Results[0].Type != "error"
	method.LastResultError = method.LastResultIndex >= 0 && method.Results[method.LastResultIndex].Type == "error"
}

func (method *FuncDecl) WriteRR(builder *strings.Builder) {
	if len(method.Params) > 0 {
		// Generate request struct to hold params
		builder.WriteString("type Request struct {\n")
		for _, param := range method.Params {
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" bson:\"%s\"`\n", GoCamelCase(param.Name), param.Type, param.Name, param.Name))
		}
		builder.WriteString("}\n")
	}
	resultLastIndex := len(method.Results) - 1
	hastNormalResult := resultLastIndex >= 0 && method.Results[0].Type != "error"
	if hastNormalResult {
		// Generate response struct to hold results
		builder.WriteString("type Response struct {\n")
		for i, ret := range method.Results {
			if i == resultLastIndex && ret.Type == "error" {
				break
			}
			builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\" bson:\"%s\"`\n", GoCamelCase(ret.Name), ret.Type, ret.Name, ret.Name))
		}
		builder.WriteString("}\n")
	}
}

type Service struct {
	Name    string
	Methods []*FuncDecl
}

type Parser struct {
	fileset  *token.FileSet
	Services map[string]*Service
	Pkgname  string

	copySpecs []ast.Spec
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
	for name, pkg := range packages {
		p.Pkgname = name
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch value := decl.(type) {
				case *ast.FuncDecl:
					if value.Doc != nil {
						if err := p.parseFuncDecl(value); err != nil {
							return err
						}
					}
				case *ast.GenDecl:
					if value.Doc != nil {
						switch value.Tok {
						case token.TYPE:
							for _, comment := range value.Doc.List {
								if strings.Contains(comment.Text, "@api") {
									p.copySpecs = append(p.copySpecs, value.Specs...)
								}
							}
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
	case *ast.IndexExpr:
		xval, err := p.exprToString(value.X)
		if err != nil {
			return "", err
		}
		ival, err := p.exprToString(value.Index)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s[%s]", xval, ival), nil
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
	for _, comment := range fdecl.Doc.List {
		if strings.Contains(comment.Text, "@api") {
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
			method := &FuncDecl{Decl: fdecl, Name: fdecl.Name.Name, LastResultIndex: -1}
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
			method.Init()
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

func (p *Parser) WriteClient(pkgname, hpath, path string) error {
	builder := &strings.Builder{}
	// buf := new(bytes.Buffer)
	// ffile := &ast.File{
	// 	Name:  ast.NewIdent(pkgname),
	// 	Decls: []ast.Decl{&ast.GenDecl{Tok: token.TYPE, Specs: p.copySpecs}},
	// }
	// if err := format.Node(buf, p.fileset, ffile); err != nil {
	// 	return err
	// }
	// log.Println(buf.String())
	builder.WriteString("package " + pkgname + "\n\n")
	builder.WriteString("import (\n")
	builder.WriteString("\t\"github.com/zdypro888/apigo\"\n")
	builder.WriteString(")\n\n")

	for name, service := range p.Services {
		clientName := name + "Client"
		// Generate struct type with service name and client instance
		builder.WriteString(fmt.Sprintf("type %s struct {\n", clientName))
		builder.WriteString("\tclient *apigo.Client\n")
		builder.WriteString("}\n")
		// Generate "New<service name>Client" function to get client instance
		builder.WriteString(fmt.Sprintf("\nfunc New%s(client *apigo.Client) *%s {\n", clientName, clientName))
		builder.WriteString(fmt.Sprintf("\treturn &%s{client: client}\n}\n", clientName))

		// Loop through methods to generate method code
		for _, method := range service.Methods {
			var paramStrings []string
			var retStrings []string
			for _, param := range method.Params {
				paramStrings = append(paramStrings, fmt.Sprintf("%s %s", param.Name, param.Type))
			}
			for _, ret := range method.Results {
				retStrings = append(retStrings, ret.Type)
			}
			if len(retStrings) == 0 || retStrings[len(retStrings)-1] != "error" {
				retStrings = append(retStrings, "error")
			}
			// Generate function doc
			for _, comment := range method.Decl.Doc.List {
				if !strings.Contains(comment.Text, "@api") {
					builder.WriteString(fmt.Sprintf("%s\n", comment.Text))
				}
			}
			// Generate function signature
			builder.WriteString(fmt.Sprintf("func (c *%s) %s(%s) (%s) {\n", clientName, method.Name, strings.Join(paramStrings, ", "), strings.Join(retStrings, ", ")))
			method.WriteRR(builder)
			if len(method.Params) > 0 {
				// Generate request object
				builder.WriteString("req := &Request{\n")
				for _, param := range method.Params {
					builder.WriteString(fmt.Sprintf("\t%s: %s,\n", GoCamelCase(param.Name), param.Name))
				}
				builder.WriteString("}\n")
			}
			writeErrResult := func(writer *strings.Builder) {
				var nilStrings []string
				for i, ret := range method.Results {
					if i == method.LastResultIndex && ret.Type == "error" {
						nilStrings = append(nilStrings, "err")
					} else {
						nilStrings = append(nilStrings, "nil")
					}
				}
				writer.WriteString("\treturn ")
				writer.WriteString(strings.Join(nilStrings, ", "))
				writer.WriteString("\n")
			}
			writeRespResult := func(writer *strings.Builder) {
				var respStrings []string
				for i, ret := range method.Results {
					if i == method.LastResultIndex && ret.Type == "error" {
						respStrings = append(respStrings, "nil")
					} else {
						respStrings = append(respStrings, fmt.Sprintf("resp.%s", GoCamelCase(ret.Name)))
					}
				}
				writer.WriteString("\treturn ")
				writer.WriteString(strings.Join(respStrings, ", "))
				writer.WriteString("\n")
			}
			// Generate request code
			if len(method.Params) == 0 {
				if !method.HasNormalResult {
					builder.WriteString(fmt.Sprintf("\tif err := apigo.Notify(c.client, \"%s/%s/%s\", http.MethodGet, nil); err != nil {\n", hpath, name, method.Name))
					builder.WriteString("\t\treturn err\n\t}\n")
					builder.WriteString("\treturn nil\n")
				} else {
					builder.WriteString(fmt.Sprintf("\tresp, err := apigo.Request[Response](c.client, \"%s/%s/%s\", http.MethodPost, nil)\n", hpath, name, method.Name))
					builder.WriteString("\tif err != nil {\n")
					writeErrResult(builder)
					builder.WriteString("\t}\n")
					writeRespResult(builder)
				}
			} else {
				if !method.HasNormalResult {
					builder.WriteString(fmt.Sprintf("\tif err := apigo.Notify(c.client, \"%s/%s/%s\", http.MethodGet, req); err != nil {\n", hpath, name, method.Name))
					builder.WriteString("\t\treturn err\n\t}\n")
					builder.WriteString("\treturn nil\n")
				} else {
					builder.WriteString(fmt.Sprintf("\tresp, err := apigo.Request[Response](c.client, \"%s/%s/%s\", http.MethodPost, &req)\n", hpath, name, method.Name))
					builder.WriteString("\tif err != nil {\n")
					writeErrResult(builder)
					builder.WriteString("\t}\n")
					writeRespResult(builder)
				}
			}
			builder.WriteString("}\n\n")
		}
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "client.go"), []byte(builder.String()), 0644); err != nil {
		return err
	}
	return nil
}

func (p *Parser) WriteServer(pkgname, hpath, path string) error {
	builder := &strings.Builder{}
	builder.WriteString("package " + pkgname)
	builder.WriteString("\n\nimport (\n")
	builder.WriteString("\t\"net/http\"\n")
	builder.WriteString("\t\"github.com/kataras/iris/v12\"\n")
	builder.WriteString("\t\"github.com/zdypro888/apigo\"\n")
	builder.WriteString(")\n\n")

	for name, service := range p.Services {
		serviceName := name + "Server"
		// Generate struct type with service name and client instance
		builder.WriteString(fmt.Sprintf("type %s struct {\n", serviceName))
		builder.WriteString("\tserver *apigo.Server\n")
		if p.Pkgname == pkgname {
			builder.WriteString(fmt.Sprintf("\t%s %s\n", service.Name, name))
		} else {
			builder.WriteString(fmt.Sprintf("\t%s %s.%s\n", service.Name, p.Pkgname, name))
		}
		builder.WriteString("}\n")
		builder.WriteString(fmt.Sprintf("\nfunc New%s(server *apigo.Server) *%s {\n", serviceName, serviceName))
		builder.WriteString(fmt.Sprintf("\ts := &%s{server: server}\n", serviceName))
		builder.WriteString("\ts.init()\n")
		builder.WriteString("\treturn s\n}\n")

		builder.WriteString(fmt.Sprintf("func (s *%s) init() {\n", serviceName))
		for _, method := range service.Methods {
			if method.HasNormalResult {
				builder.WriteString(fmt.Sprintf("\ts.server.HandleRequest(\"%s/%s/%s\", s.handle%s)\n", hpath, name, method.Name, method.Name))
			} else {
				builder.WriteString(fmt.Sprintf("\ts.server.HandleNotify(\"%s/%s/%s\", s.handle%s)\n", hpath, name, method.Name, method.Name))
			}
		}
		builder.WriteString("}\n\n")
		for _, method := range service.Methods {
			builder.WriteString(fmt.Sprintf("func (s *%s) handle%s(ctx iris.Context) {\n", serviceName, method.Name))
			method.WriteRR(builder)
			if len(method.Params) > 0 {
				// Generate request object
				builder.WriteString("req, err := apigo.ReadMessage[Request](s.server, ctx)\n")
				builder.WriteString("if err != nil {\n")
				builder.WriteString("\ts.server.ResponseError(ctx, 500, err)\n")
				builder.WriteString("\treturn\n")
				builder.WriteString("}\n")
			}
			var paramStrings []string
			for _, param := range method.Params {
				paramStrings = append(paramStrings, fmt.Sprintf("req.%s", GoCamelCase(param.Name)))
			}
			if method.HasNormalResult {
				if method.LastResultError && len(method.Params) == 0 {
					builder.WriteString("var err error\n")
				}
				builder.WriteString("var resp Response\n")
				var retStrings []string
				for i, ret := range method.Results {
					if i == method.LastResultIndex && ret.Type == "error" {
						retStrings = append(retStrings, "err")
					} else {
						retStrings = append(retStrings, fmt.Sprintf("resp.%s", GoCamelCase(ret.Name)))
					}
				}
				builder.WriteString(strings.Join(retStrings, ", "))
				builder.WriteString(" = s.")
			} else if method.LastResultError {
				if len(method.Params) > 0 {
					builder.WriteString("err = s.")
				} else {
					builder.WriteString("err := s.")
				}
			}
			builder.WriteString(service.Name)
			builder.WriteString(".")
			builder.WriteString(method.Name)
			builder.WriteString("(")
			builder.WriteString(strings.Join(paramStrings, ", "))
			builder.WriteString(")\n")
			if method.LastResultError {
				builder.WriteString("if err != nil {\n")
				builder.WriteString("\ts.server.ResponseError(ctx, 501, err)\n")
				builder.WriteString("\treturn\n")
				builder.WriteString("}\n")
			}
			if method.HasNormalResult {
				builder.WriteString("s.server.ResponseData(ctx, resp)\n")
			} else {
				builder.WriteString("s.server.ResponseData(ctx, nil)\n")
			}
			builder.WriteString("}\n\n")
		}
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "server.go"), []byte(builder.String()), 0644); err != nil {
		return err
	}
	return nil
}

func (p *Parser) WriteJavascript(path string) error {
	builder := &strings.Builder{}
	builder.WriteString("import { Client } from \"./client\";\n\n")
	for name, service := range p.Services {
		serviceName := name + "Client"
		// Generate struct type with service name and client instance
		builder.WriteString(fmt.Sprintf("export class %s extends Client {\n", serviceName))
		builder.WriteString(fmt.Sprintf("\t%s: %s;\n", service.Name, name))
		builder.WriteString(fmt.Sprintf("\tconstructor(url: string, %s: %s) {\n", service.Name, name))
		builder.WriteString("\t\tsuper(url);\n")
		builder.WriteString(fmt.Sprintf("\t\tthis.%s = %s;\n", service.Name, service.Name))
		builder.WriteString("\t}\n")
		for _, method := range service.Methods {
			builder.WriteString(fmt.Sprintf("\thandle%s(data: any, callback: (err: any, result: any) => void) {\n", method.Name))
			method.WriteRR(builder)
			if len(method.Params) > 0 {
				// Generate request object
				builder.WriteString("var req = new Request();\n")
				for _, param := range method.Params {
					builder.WriteString(fmt.Sprintf("req.%s = data.%s;\n", GoCamelCase(param.Name), GoCamelCase(param.Name)))
				}
			}
			var paramStrings []string
			for _, param := range method.Params {
				paramStrings = append(paramStrings, fmt.Sprintf("req.%s", GoCamelCase(param.Name)))
			}
			if method.HasNormalResult {
				if method.LastResultError && len(method.Params) == 0 {
					builder.WriteString("var err: any;\n")
				}
				builder.WriteString("var resp = new Response();\n")
				var retStrings []string
				for i, ret := range method.Results {
					if i == method.LastResultIndex && ret.Type == "error" {
						retStrings = append(retStrings, "err")
					} else {
						retStrings = append(retStrings, fmt.Sprintf("resp.%s", GoCamelCase(ret.Name)))
					}
				}
				builder.WriteString(strings.Join(retStrings, ", "))
				builder.WriteString(" = this.")
			} else if method.LastResultError {
				if len(method.Params) > 0 {
					builder.WriteString("var err: any = this.")
				} else {
					builder.WriteString("var err: any = this.")
				}
			}
			builder.WriteString(service.Name)
			builder.WriteString(".")
			builder.WriteString(method.Name)
			builder.WriteString("(")
			builder.WriteString(strings.Join(paramStrings, ", "))
			builder.WriteString(");\n")
			if method.LastResultError {
				builder.WriteString("if (err) {\n")
				builder.WriteString("\t\tcallback(err, null);\n")
				builder.WriteString("\t\treturn;\n")
				builder.WriteString("\t}\n")
			}
			if method.HasNormalResult {
				builder.WriteString("\t\tcallback(null, resp);\n")
			} else {
				builder.WriteString("\t\tcallback(null, null);\n")
			}
			builder.WriteString("\t}\n\n")
		}
		builder.WriteString("}\n\n")
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "client.ts"), []byte(builder.String()), 0644); err != nil {
		return err
	}
	return nil
}
