package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func main() {
	api := &ApiInfo{}
	api.Parse("/Users/zdypro/Documents/projects/src/zdypro888/applesys/nserver/account.go")
}

type Param struct {
	Name string
	Type string
}

type APIParam struct {
	Key string `json:"key"`
}

type APIDescript struct {
	Method string               `json:"method"`
	Path   string               `json:"path"`
	Query  map[string]*APIParam `json:"query"`
	Param  map[string]*APIParam `json:"param"`
	Form   map[string]*APIParam `json:"form"`
}

type ApiInfo struct {
	PackageName string
	fileSet     *token.FileSet
	servers     []*strings.Builder
	clients     []*strings.Builder
}

func (api *ApiInfo) Parse(filename string) error {
	api.fileSet = token.NewFileSet()
	astFile, err := parser.ParseFile(api.fileSet, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, decl := range astFile.Decls {
		switch value := decl.(type) {
		case *ast.FuncDecl:
			if value.Doc != nil {
				if err = api.function(value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (api *ApiInfo) function(fdecl *ast.FuncDecl) error {
	for _, doc := range fdecl.Doc.List {
		if strings.HasPrefix(doc.Text, "// @api ") {
			apiJSON := strings.TrimPrefix(doc.Text, "// @api ")
			var apiDesc APIDescript
			if err := json.Unmarshal([]byte(apiJSON), &apiDesc); err != nil {
				return err
			}
			swriter := &strings.Builder{}
			swriter.WriteString("func handle")
			swriter.WriteString(fdecl.Name.Name)
			swriter.WriteString("(c *gin.Context) {\n")
			var params []*Param
			for _, paramItem := range fdecl.Type.Params.List {
				if paramItemIdent, ok := paramItem.Type.(*ast.Ident); ok {
					for _, nameIdent := range paramItem.Names {
						param := &Param{
							Name: nameIdent.Name,
							Type: paramItemIdent.Name,
						}
						for name, par := range apiDesc.Query {
							if name == param.Name {
								swriter.WriteString(param.Name)
								swriter.WriteString("Str := c.Query(\"")
								swriter.WriteString(par.Key)
								swriter.WriteString("\")\n")
								break
							}
						}
						for name, par := range apiDesc.Param {
							if name == param.Name {
								swriter.WriteString(param.Name)
								swriter.WriteString("Str := c.Param(\"")
								swriter.WriteString(par.Key)
								swriter.WriteString("\")\n")
								break
							}
						}
						for name, par := range apiDesc.Form {
							if name == param.Name {
								swriter.WriteString(param.Name)
								swriter.WriteString("Str := c.PostForm(\"")
								swriter.WriteString(par.Key)
								swriter.WriteString("\")\n")
								break
							}
						}
						switch param.Type {
						case "string":
							swriter.WriteString(param.Name)
							swriter.WriteString(" := ")
							swriter.WriteString(param.Name)
							swriter.WriteString("Str\n")
						case "int", "int8", "int16", "int32", "int64":
							swriter.WriteString(param.Name)
							swriter.WriteString("Int64, err := strconv.ParseInt(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Str, 10, 64)\n")
							swriter.WriteString("if err != nil {\n")
							swriter.WriteString("c.JSON(http.StatusBadRequest, GlobalResponse{")
							swriter.WriteString("Code: 400,")
							swriter.WriteString("Msg: \"")
							swriter.WriteString(param.Name)
							swriter.WriteString(" must be ")
							swriter.WriteString(param.Type)
							swriter.WriteString("\"")
							swriter.WriteString("})\n")
							swriter.WriteString("return\n")
							swriter.WriteString("}\n")
							swriter.WriteString(param.Name)
							swriter.WriteString(" := ")
							swriter.WriteString(param.Type)
							swriter.WriteString("(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Int64)\n")
						case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
							swriter.WriteString(param.Name)
							swriter.WriteString("Int64, err := strconv.ParseUint(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Str, 10, 64)\n")
							swriter.WriteString("if err != nil {\n")
							swriter.WriteString("c.JSON(http.StatusBadRequest, GlobalResponse{")
							swriter.WriteString("Code: 400, ")
							swriter.WriteString("Msg: \"")
							swriter.WriteString(param.Name)
							swriter.WriteString(" must be ")
							swriter.WriteString(param.Type)
							swriter.WriteString("\",")
							swriter.WriteString("})\n")
							swriter.WriteString("return\n")
							swriter.WriteString("}\n")
							swriter.WriteString(param.Name)
							swriter.WriteString(" := ")
							swriter.WriteString(param.Type)
							swriter.WriteString("(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Int64)\n")
						case "float32", "float64":
							swriter.WriteString(param.Name)
							swriter.WriteString("Float64, err := strconv.ParseFloat(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Str, 64)\n")
							swriter.WriteString("if err != nil {\n")
							swriter.WriteString("c.JSON(http.StatusBadRequest, GlobalResponse{")
							swriter.WriteString("Code: 400, ")
							swriter.WriteString("Msg: \"")
							swriter.WriteString(param.Name)
							swriter.WriteString(" must be ")
							swriter.WriteString(param.Type)
							swriter.WriteString("\"")
							swriter.WriteString("})\n")
							swriter.WriteString("return\n")
							swriter.WriteString("}\n")
							swriter.WriteString(param.Name)
							swriter.WriteString(" := ")
							swriter.WriteString(param.Type)
							swriter.WriteString("(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Float64)\n")
						case "bool":
							swriter.WriteString(param.Name)
							swriter.WriteString("Bool, err := strconv.ParseBool(")
							swriter.WriteString(param.Name)
							swriter.WriteString("Str)\n")
							swriter.WriteString("if err != nil {\n")
							swriter.WriteString("c.JSON(http.StatusBadRequest, GlobalResponse{")
							swriter.WriteString("Code: 400, ")
							swriter.WriteString("Msg: \"")
							swriter.WriteString(param.Name)
							swriter.WriteString(" must be bool\"")
							swriter.WriteString("})\n")
							swriter.WriteString("return\n")
							swriter.WriteString("}\n")
							swriter.WriteString(param.Name)
							swriter.WriteString(" := ")
							swriter.WriteString(param.Name)
							swriter.WriteString("Bool\n")
						}
						params = append(params, param)
					}
				}
			}
			swriter.WriteString("result, err := ")
			swriter.WriteString(fdecl.Name.Name)
			swriter.WriteString("(")
			for i, param := range params {
				swriter.WriteString(param.Name)
				if i != len(params)-1 {
					swriter.WriteString(",")
				}
			}
			swriter.WriteString(")\n")
			swriter.WriteString("if err != nil {\n")
			swriter.WriteString("c.JSON(http.StatusInternalServerError, GlobalResponse{")
			swriter.WriteString("Code: 500, ")
			swriter.WriteString("Msg: err.Error()")
			swriter.WriteString("})\n")
			swriter.WriteString("return\n")
			swriter.WriteString("}\n")
			swriter.WriteString("c.JSON(http.StatusOK, GlobalResponse{")
			swriter.WriteString("Code: 0, ")
			swriter.WriteString("Data: result")
			swriter.WriteString("})\n")
			swriter.WriteString("}\n")
			api.servers = append(api.servers, swriter)

			cwriter := &strings.Builder{}
			cwriter.WriteString("func ")
			cwriter.WriteString(fdecl.Name.Name)
			cwriter.WriteString("(")
			for i, param := range params {
				cwriter.WriteString(param.Name)
				cwriter.WriteString(" ")
				cwriter.WriteString(param.Type)
				if i != len(params)-1 {
					cwriter.WriteString(",")
				}
			}
			cwriter.WriteString(") (")
			for i, resultItem := range fdecl.Type.Results.List {
				switch Ident := resultItem.Type.(type) {
				case *ast.Ident:
					cwriter.WriteString(Ident.Name)
				case *ast.StarExpr:
					cwriter.WriteString("*")
					switch xIdent := Ident.X.(type) {
					case *ast.Ident:
						cwriter.WriteString(xIdent.Name)
					case *ast.SelectorExpr:
						if xxIdent, ok := xIdent.X.(*ast.Ident); ok {
							cwriter.WriteString(xxIdent.Name)
							cwriter.WriteString(".")
						}
						cwriter.WriteString(xIdent.Sel.Name)
					}
				}
				if i != len(fdecl.Type.Results.List)-1 {
					cwriter.WriteString(", ")
				}
			}
			cwriter.WriteString(") {\n")
			apipath := apiDesc.Path
			if len(apiDesc.Query) > 0 {
				apipath += "?"
			}
			datawriter := &strings.Builder{}
			for _, param := range params {
				for name, par := range apiDesc.Query {
					if name == param.Name {
						apipath += par.Key + "=" + param.Name + "&"
						break
					}
				}
				for name, par := range apiDesc.Param {
					if name == param.Name {
						apipath = strings.ReplaceAll(apipath, ":"+par.Key, "\"+fmt.Sprintf(\"%v\", "+param.Name+")+\"")
						break
					}
				}
				for name, par := range apiDesc.Form {
					if name == param.Name {
						datawriter.WriteString(par.Key)
						break
					}
				}
			}
			cwriter.WriteString("url := apiHost + \"")
			cwriter.WriteString(apipath)
			cwriter.WriteString("\"\n")
			cwriter.WriteString("method := ")
			cwriter.WriteString("\"")
			cwriter.WriteString(apiDesc.Method)
			cwriter.WriteString("\"\n")
			cwriter.WriteString("var data []byte\n")
			for name := range apiDesc.Form {
				for _, param := range params {
					if name == param.Name {
						cwriter.WriteString("data, err = json.Marshal(")
						cwriter.WriteString(param.Name)
						cwriter.WriteString(")\n")
						cwriter.WriteString("if err != nil {\n")
						cwriter.WriteString("return nil, err\n")
						cwriter.WriteString("}\n")
						break
					}
				}
			}
			cwriter.WriteString("res, err := httpclient.RequestMethod(context.Background(), url, method, nil, data)\n")
			cwriter.WriteString("if err != nil {\n")
			cwriter.WriteString("return nil, err\n")
			cwriter.WriteString("}\n")
			cwriter.WriteString("if res.StatusCode != 200 {\n")
			cwriter.WriteString("return nil, res\n")
			cwriter.WriteString("}\n")
			cwriter.WriteString("var body []byte\n")
			cwriter.WriteString("if body, err = res.Data(); err != nil {\n")
			cwriter.WriteString("return nil, err\n")
			cwriter.WriteString("}\n")
			cwriter.WriteString("var result ")
			for i, resultItem := range fdecl.Type.Results.List {
				switch Ident := resultItem.Type.(type) {
				case *ast.Ident:
					cwriter.WriteString(Ident.Name)
				case *ast.StarExpr:
					cwriter.WriteString("*")
					switch xIdent := Ident.X.(type) {
					case *ast.Ident:
						cwriter.WriteString(xIdent.Name)
					case *ast.SelectorExpr:
						if xxIdent, ok := xIdent.X.(*ast.Ident); ok {
							cwriter.WriteString(xxIdent.Name)
							cwriter.WriteString(".")
						}
						cwriter.WriteString(xIdent.Sel.Name)
					}
				}
				if i != len(fdecl.Type.Results.List)-1 {
					cwriter.WriteString(", ")
				}
			}
			cwriter.WriteString("\n")
			cwriter.WriteString("err = json.Unmarshal(res, &result)\n")
			cwriter.WriteString("if err != nil {\n")
			cwriter.WriteString("return nil, err\n")
			cwriter.WriteString("}\n")
			cwriter.WriteString("if result.Code != 0 {\n")
			cwriter.WriteString("return nil, errors.New(result.Msg)\n")
			cwriter.WriteString("}\n")
			cwriter.WriteString("return result.Data, nil\n")
			cwriter.WriteString("}\n")
			fmt.Println(cwriter.String())
			api.clients = append(api.clients, cwriter)
		}
	}
	return nil
}

func (api *ApiInfo) Write() {
	writer := &strings.Builder{}
	writer.WriteString("package ")
	writer.WriteString(api.PackageName)
	writer.WriteString("type GlobalResponse struct {\n")
	writer.WriteString("Code int         `json:\"code\"`\n")
	writer.WriteString("Msg  string      `json:\"msg\"`\n")
	writer.WriteString("Data interface{} `json:\"data\"`\n")
	writer.WriteString("}\n")

	for _, server := range api.servers {
		writer.WriteString(server.String())
	}
}
